package main

import (
	"fmt"
	. "github.com/dave/jennifer/jen"
	"github.com/iancoleman/strcase"
	"net/http"
	"os"
	"sort"
	"strings"
)

func exit(code int, s interface{}) {
	fmt.Println(s)
	os.Exit(code)
}

func guard(err error) {
	if err != nil {
		exit(1, err)
	}
}

func contains(needle string, haystack []string) bool {
	found := false
	for _, hay := range haystack {
		if hay == needle {
			found = true
			break
		}
	}
	return found
}

func sortedKeys(m map[string]interface{}) []string {
	keys := make([]string, len(m))
	i := 0
	for k := range m {
		keys[i] = k
		i++
	}
	sort.Strings(keys)
	return keys
}

func renderId(name string) *Statement {
	return Id(strcase.ToCamel(name))
}

func newRequest(responseExampleRequest ResponseExampleRequest) (*http.Request, error) {
	req, err := http.NewRequest("GET", "https://next.sonarqube.com/sonarqube/api/webservices/response_example", nil)
	if err != nil {
		return nil, err
	}

	q := req.URL.Query()
	q.Add("action", responseExampleRequest.Action)
	q.Add("controller", responseExampleRequest.Controller)

	req.URL.RawQuery = q.Encode()

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	return req, nil
}

func qualifier(pkg string) string {
	return fmt.Sprintf("github.com/shijl0925/go-sonarqube/sonarqube/%s", pkg)
}

func ifTrueGen(ok bool, statement *Statement) *Statement {
	if ok {
		return statement
	} else {
		return Empty()
	}
}

func ifTrueGenOrNil(ok bool, statement *Statement) *Statement {
	if ok {
		return statement
	} else {
		return Nil()
	}
}

func ifErrorReturn(ok bool) *Statement {
	if ok {
		return If(Err().Op("!=").Nil()).Block(
			Return(Nil().Op(",").Op("err")),
		)
	} else {
		return If(Err().Op("!=").Nil()).Block(
			Return(Err()),
		)
	}
}

func genReturnWithError(hasRetVal bool, retId string) *Statement {
	if hasRetVal {
		return Return(Id(retId).Op(",").Nil())
	} else {
		return Return(Nil())
	}
}

func errResult(action Action, errStmt *Statement) *Statement {
	if action.HasResponseExample {
		return Return(Nil().Op(",").Add(errStmt))
	} else {
		return Return(errStmt)
	}
}

func ifError(action Action, s string) *Statement {
	return If(Err().Op("!=").Nil()).Block(
		errResult(action,
			Qual("fmt", "Errorf").Call(
				Lit(s),
				Err(),
			),
		),
	)
}

func replaceTags(str string) string {
	repl := strings.NewReplacer(
		"\n", "\n// ",
		"<br> ", "\n// ",
		"<br>", "\n// ",
		"<br/>", "\n// ",
		"<br />", "\n// ",
		"<ul>", "\n// ",
		"<li>", " * ",
		"</li>", "\n// ",
		"</ul>", "",
	)
	return repl.Replace(str)
}
