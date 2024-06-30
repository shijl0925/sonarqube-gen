package main

import (
	"bytes"
	"fmt"
	"go/format"
	"io"
	"log"
	"text/template"
)

const (
	clientTemplateName     = "sonarqube.tpl"
	clientTemplateFileName = "./gen/tpl/sonarqube.tpl"
	clientFileName         = "sonarqube.go"
)

var (
	clientTemplate = template.Must(template.New(clientTemplateName).ParseFiles(clientTemplateFileName))
)

func renderClient(in io.Writer, data *Api) error {
	buff := bytes.NewBuffer([]byte{})

	if err := clientTemplate.Execute(buff, data); err != nil {
		return fmt.Errorf("failed to render client: %w", err)
	}

	src := buff.Bytes()

	formatted, err := format.Source(src)
	if err != nil {
		log.Printf("failed to format source of sonarqube.go: err:%s", err.Error())
		formatted = src
	}

	_, err = in.Write(formatted)
	return err
}
