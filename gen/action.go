package main

import (
	"encoding/json"
	"fmt"
	. "github.com/dave/jennifer/jen"
	"github.com/iancoleman/strcase"
	"io/ioutil"
	"net/http"
	"reflect"
	"time"
)

type Action struct {
	Key                string  `json:"key"`
	Description        string  `json:"description"`
	Internal           bool    `json:"internal"`
	Post               bool    `json:"post"`
	HasResponseExample bool    `json:"hasResponseExample"`
	Params             []Param `json:"params"`
	ChangeLog          []struct {
		Version     string `json:"version"`
		Description string `json:"description"`
	} `json:"changelog"`
	Since           string `json:"since"`
	DeprecatedSince string `json:"deprecatedSince"`
}

type Param struct {
	Key             string `json:"key"`
	Description     string `json:"description"`
	Internal        bool   `json:"internal"`
	Required        bool   `json:"required"`
	Since           string `json:"since"`
	DeprecatedSince string `json:"deprecatedSince"`
}

func (p *Param) render(post bool) *Statement {
	var tag string
	if post {
		tag = "form"
	} else {
		tag = "url"
	}

	key := p.Key
	if !p.Required {
		key = fmt.Sprintf("%s,omitempty", key)
	}

	var comment string
	if p.Since != "" {
		comment += fmt.Sprintf("Since %s;", p.Since)
	}
	if p.DeprecatedSince != "" {
		comment += fmt.Sprintf("Deprecated since %s;", p.DeprecatedSince)
	}
	if p.Description != "" {
		comment += p.Description
	}
	return renderId(p.Key).String().Tag(map[string]string{tag: key}).Comment(comment)
}

type ResponseExampleRequest struct {
	ID         string
	RequestID  string
	Action     string
	Controller string
}

type Example struct {
	Format  string `json:"format"`
	Example string `json:"example"` // yes, it's a string...
}

func (a *Action) id() string {
	return strcase.ToCamel(a.Key)
}

func (a *Action) requestTypeName() string {
	return fmt.Sprintf("%s%s", a.id(), "Request")
}

func (a *Action) responseTypeName() string {
	return fmt.Sprintf("%s%s", a.id(), "Response")
}

func (a *Action) responseAllTypeName() string {
	return fmt.Sprintf("%s%s", a.id(), "ResponseAll")
}

func (a *Action) pagingFuncName() string {
	return "GetPaging"
}

func (a *Action) serviceFuncName() string {
	return a.id()
}

func (a *Action) serviceAllFuncName() string {
	return fmt.Sprintf("%s%s", a.id(), "All")
}

func (a *Action) hasPaging() bool {
	hasP := false
	hasPs := false

	for _, param := range a.Params {
		switch param.Key {
		case "p":
			hasP = true
		case "ps":
			hasPs = true
		}
	}

	return hasP && hasPs
}

type ResponseFieldsGenerator struct {
	parser *FieldParser
}

func NewResponseFieldsGenerator(parser *FieldParser) *ResponseFieldsGenerator {
	return &ResponseFieldsGenerator{parser: parser}
}

func (g *ResponseFieldsGenerator) generate(responseTypeName string, example interface{}) (Field, error) {
	if reflect.TypeOf(example) == reflect.TypeOf([]interface{}{}) {
		return g.parser.NewSliceField(responseTypeName, example.([]interface{})), nil
	} else {
		if _, ok := example.(map[string]interface{})["format"]; ok {
			return &StringField{name: responseTypeName}, nil
		}
		return g.parser.NewMapField(responseTypeName, example.(map[string]interface{})), nil
	}
}

func (g *ResponseFieldsGenerator) generatedWithoutPaging(responseAllTypeName string, example map[string]interface{}) (Field, error) {
	delete(example, "paging")

	// Remove flattened paging as well
	delete(example, "p")
	delete(example, "ps")
	delete(example, "total")

	return g.parser.NewMapField(responseAllTypeName, example), nil
}

type RequestStructGenerator struct {
	service *Service
	action  *Action
}

func NewRequestStructGenerator(service *Service, action *Action) *RequestStructGenerator {
	return &RequestStructGenerator{service: service, action: action}
}

func (g *RequestStructGenerator) generate() *Statement {
	fields := make([]Code, len(g.action.Params))
	for i, param := range g.action.Params {
		// filter out unwanted fields and paging parameters
		if contains(param.Key, append(skippedRequestFields, "p", "ps")) {
			continue
		}

		fields[i] = param.render(g.action.Post)
	}

	statement := Commentf("%s %s", g.action.requestTypeName(), g.action.Description)
	if g.action.DeprecatedSince != "" {
		statement.Line()
		statement.Commentf("Deprecated: this action has been deprecated since version %s", g.action.DeprecatedSince)
	}
	statement.Line()

	statement.Type().Id(g.action.requestTypeName()).Struct(fields...)

	return statement
}

func (a *Action) responseStruct(response Field) *Statement {
	// EmptyField should not be rendered
	if reflect.TypeOf(response) != reflect.TypeOf(&EmptyField{}) {
		fields := response.Render(false)
		statement := Commentf("%s is the response for %s", a.responseTypeName(), a.requestTypeName())
		statement.Line()
		statement.Type().Add(fields)
		return statement
	}

	return Empty()
}

func (a *Action) responseStructPagingFunc(collection Field) *Statement {
	// EmptyField should not have a Paging
	if reflect.TypeOf(collection) == reflect.TypeOf(&MapField{}) {
		statement := Commentf("%s extracts the paging from %s", a.pagingFuncName(), a.responseTypeName())
		statement.Line()
		statement.Func().Parens(Id("r").Op("*").Id(a.responseTypeName())).Id(a.pagingFuncName()).Call().Op("*").Qual(qualifier("paging"), "Paging")

		if contains("Paging", collection.(*MapField).Accessors()) {
			statement.Block(Return(Op("&").Id("r").Dot("Paging")))
		} else {
			statement.Block(Return(
				Op("&").Qual(qualifier("paging"), "Paging").Block(Dict{
					//Id("PageIndex"): Int().Parens(Id("r").Dot("P")),
					//Id("PageSize"):  Int().Parens(Id("r").Dot("Ps")),
					//Id("Total"):     Int().Parens(Id("r").Dot("Total")),
				}),
			))
		}

		return statement
	}

	return Empty()
}

func (a *Action) responseAllStruct(collection Field) *Statement {
	// EmptyField should not be rendered
	if reflect.TypeOf(collection) != reflect.TypeOf(&EmptyField{}) {
		fields := collection.Render(false)
		statement := Commentf("%s is the collection for %s", a.responseAllTypeName(), a.requestTypeName())
		statement.Line()
		statement.Type().Add(fields)
		return statement
	}

	return Empty()
}

// checkJSONType 动态判断JSON字符串的类型，并返回相应的接口类型。
func checkJSONType(body []byte) interface{} {
	// 预防空切片导致的panic
	if len(body) == 0 {
		return nil
	}

	// 使用更健壮的方式判断JSON类型，这里以检查是否以"["开头作为切片的简要示例
	// 实际应用中可能需要更复杂的逻辑，比如解析部分JSON来确定结构
	if body[0] == '[' {
		return []interface{}{}
	}
	return map[string]interface{}{}
}

func (a *Action) fetchExample(endpoint string) (interface{}, error) {
	controller := fmt.Sprintf("api/%s", endpoint)
	request := ResponseExampleRequest{ID: a.responseTypeName(), RequestID: a.id(), Controller: controller, Action: a.Key}

	client := &http.Client{Timeout: 10 * time.Second}

	req, err := newRequest(request)
	if err != nil {
		return nil, fmt.Errorf("could not create request: %+v", err)
	}

	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error sending reqeust: %+v", err)
	}
	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("could not read response body: %+v", err)
	}

	var responseExample Example
	err = json.Unmarshal(body, &responseExample)
	if err != nil {
		return nil, fmt.Errorf("could not unmarshall body: %+v", err)
	}

	if responseExample.Format == "json" {
		// Convert the example JSON string (!!) to a map
		example := checkJSONType([]byte(responseExample.Example))
		if example == nil {
			return nil, fmt.Errorf("无法确定JSON类型：空输入")
		}

		err := json.Unmarshal([]byte(responseExample.Example), &example)
		if err != nil {
			return nil, fmt.Errorf("could not marshall example: %+v", err)
		}

		return example, nil
	} else if responseExample.Format == "txt" || responseExample.Format == "xml" || responseExample.Format == "svg" || responseExample.Format == "log" || responseExample.Format == "proto" {
		// parse txt / xml / svg / log response
		example := map[string]interface{}{
			"example": responseExample.Example,
			"format":  responseExample.Format,
		}
		return example, nil
	} else {
		return nil, fmt.Errorf("unsupported response format %s", responseExample.Format)
	}
}
