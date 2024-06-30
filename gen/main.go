// This small program is used to generate request structs and services for the SonarQube API.
// It expects a JSON file with the same structure as returned by `https://next.sonarqube.com/sonarqube/web_api/api/webservices/list`.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
)

type Api struct {
	Services []Service `json:"webServices"`
}

// These endpoints cannot/should not be generated
var skippedEndpoints = []string{
	"duplications", // numeric map keys cause parse errors
	"properties",   // unmarshall errors on already deprecated endpoint
	"favourites",   // deprecated in favour of favorites ;)
	"paging",       // non-existent, but there to prevent overwriting custom paging
}

// These fields don't need to be in each request struct
var skippedRequestFields = []string{}

const packageName = "sonarqube"

const (
	webservicesUrl     = "/api/webservices/list"
	includeInternalUrl = "?include_internals=true"
)

var (
	host     string
	internal bool
	help     bool
	auth     string
)

func main() {
	var mainFlagsSet = flag.NewFlagSet("", flag.PanicOnError)
	mainFlagsSet.StringVar(&host, "host", "http://localhost:9000", "SonarQube server")
	mainFlagsSet.BoolVar(&internal, "internal", false, "generate code for internal methods (default: false)")
	mainFlagsSet.BoolVar(&help, "help", false, "show usage")
	mainFlagsSet.StringVar(&auth, "auth", "", "the header Authorization value,example: Basic YWRtaW46YWRtaW4=")
	mainFlagsSet.Parse(os.Args[1:])
	if help {
		mainFlagsSet.Usage()
		os.Exit(0)
	}

	apiUrl := host + webservicesUrl
	if internal {
		apiUrl += includeInternalUrl
	}

	req, err := http.NewRequest("GET", apiUrl, nil)
	if err != nil {
		fmt.Printf("failed to create request: %v", err)
		exit(1, err)
	}
	if auth != "" {
		req.Header.Add("Authorization", auth)
	}
	client := http.Client{
		Timeout: 15 * time.Second, // 设置超时时间
	}
	resp, err := client.Do(req)

	if resp.StatusCode == 401 {
		fmt.Println("Authorization failed")
		os.Exit(1)
	}

	if err != nil {
		fmt.Printf("failed to fetch api definitions：%v", err)
		exit(1, err)
	}
	defer resp.Body.Close()

	var api Api
	if err := json.NewDecoder(resp.Body).Decode(&api); err != nil {
		fmt.Errorf("could not decode response: %+v", err)
	}

	// create sonarqube.go
	path := fmt.Sprintf("%s/%s", packageName, clientFileName)
	file, err := os.Create(path)
	if err != nil {
		fmt.Errorf("failed to create file：%w", err)
	}

	if err := renderClient(
		file,
		&api,
	); err != nil {
		fmt.Printf("failed to render client: %s", err.Error())
	}

	wg := &sync.WaitGroup{}
	for _, service := range api.Services {
		fmt.Printf("processing service at path %s\n", service.Path)		s := service

		s := service

		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.process(packageName)
			if err != nil {
				fmt.Printf("Error processing service at path %s: %+v\n", s.Path, err)
			}
		}()
	}

	wg.Wait()
}
