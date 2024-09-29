package main

import (
	"fmt"
	. "github.com/dave/jennifer/jen"
	"github.com/iancoleman/strcase"
	"os"
	"reflect"
	"strings"
)

const urlPrefix = "api/"

type Service struct {
	Path        string   `json:"path"`
	Description string   `json:"description"`
	Actions     []Action `json:"actions"`
}

func (s *Service) Getter() string {
	return strcase.ToCamel(s.endpoint())
}

func (s *Service) endpoint() string {
	path := strings.Split(s.Path, "/")
	return path[len(path)-1]
}

func (s *Service) process(output string) error {
	overrides := NewOverrides()

	endpoint := s.endpoint()
	if contains(endpoint, skippedEndpoints) {
		fmt.Printf("Skipping endpoint '%s'\n", endpoint)
		return nil
	}

	typesFile := NewFile(endpoint)
	typesFile.Commentf("// AUTOMATICALLY GENERATED, DO NOT EDIT BY HAND!\n")

	serviceFile := NewFile(packageName)
	serviceFile.ImportName(qualifier(endpoint), endpoint)
	serviceFile.ImportName(qualifier("paging"), "paging")
	serviceFile.Commentf("// AUTOMATICALLY GENERATED, DO NOT EDIT BY HAND!\n")

	serviceType := Type().Id(s.Getter()).Id("service")
	serviceFile.Add(serviceType)

	for _, action := range s.Actions {
		if s.Path == "api/sources" && action.Key == "index" {
			continue
		}
		fmt.Printf("Processing '%s' - '%s'\n", s.endpoint(), action.Key)

		requestStructGenerator := NewRequestStructGenerator(s, &action)
		requestStruct := requestStructGenerator.generate()
		typesFile.Add(requestStruct)

		var responseField Field = &EmptyField{}
		var responseFieldWithoutPaging Field = &EmptyField{}
		if action.HasResponseExample {
			example, err := action.fetchExample(endpoint)
			if err != nil {
				return fmt.Errorf("could not fetch example: %+v", err)
			}

			parser := NewFieldParser(s, &action, overrides.Filter(s.endpoint(), action.Key))
			responseFieldsGenerator := NewResponseFieldsGenerator(parser)

			responseField, err = responseFieldsGenerator.generate(action.responseTypeName(), example)
			if err != nil {
				return fmt.Errorf("could not collect response fields: %+v", err)
			}

			if action.hasPaging() {
				responseFieldWithoutPaging, err = responseFieldsGenerator.generatedWithoutPaging(action.responseAllTypeName(), example.(map[string]interface{}))
				if err != nil {
					return fmt.Errorf("could not extract collection field: %+v", err)
				}
			}
		}

		responseStruct := action.responseStruct(responseField)
		typesFile.Add(responseStruct)

		if action.hasPaging() {
			pagingFunc := action.responseStructPagingFunc(responseField)
			typesFile.Add(pagingFunc)
		}

		responseAllStruct := action.responseAllStruct(responseFieldWithoutPaging)
		typesFile.Add(responseAllStruct)

		// Service file
		if action.Post {
			postActionOutput := s.postServiceFunc(action, endpoint)
			serviceFile.Add(postActionOutput)
		} else {
			getActionOutput := s.getServiceFunc(action, endpoint)
			serviceFile.Add(getActionOutput)
		}

		if action.hasPaging() {
			getPagedActionOutput := s.getAllServiceFunc(action, endpoint, responseFieldWithoutPaging)
			serviceFile.Add(getPagedActionOutput)
		}
	}

	dir := fmt.Sprintf("%s/%s", output, endpoint)
	_ = os.Mkdir(dir, 0755)

	typesFileName := fmt.Sprintf("%s/%s/%s_gen.go", output, endpoint, endpoint)
	err := typesFile.Save(typesFileName)
	if err != nil {
		return fmt.Errorf("could not save generated source file for types: %+v\n", err)
	}

	serviceFileName := fmt.Sprintf("%s/%s_gen.go", output, endpoint)
	err = serviceFile.Save(serviceFileName)
	if err != nil {
		return fmt.Errorf("could not save generated source file for service: %+v\n", err)
	}

	return nil
}

func (s *Service) postServiceFunc(action Action, endpoint string) *Statement {
	// start function signature without return type
	comment := fmt.Sprintf("// %s - %s", action.serviceFuncName(), replaceTags(action.Description))
	if action.Since != "" {
		comment += fmt.Sprintf("\n// Since %s", action.Since)
	}
	if action.DeprecatedSince != "" {
		comment += fmt.Sprintf("\n// Deprecated since %s", action.DeprecatedSince)
	}
	if action.ChangeLog != nil && len(action.ChangeLog) > 0 {
		comment += "\n// Changelog:\n//"
		for _, change := range action.ChangeLog {
			comment += fmt.Sprintf("\n//\t%s: %s", change.Version, replaceTags(change.Description))
		}
	}
	statement := Comment(comment).Line()

	// func(s *<service id>) <action id>(r <request type>)
	statement.Func().Parens(Id("s").Op("*").Id(s.Getter())).Id(action.serviceFuncName())
	statement.Params(
		Id("ctx").Qual("context", "Context"),
		Id("r").Qual(qualifier(endpoint), action.requestTypeName()),
	)

	// add return type based on whether we expect a response
	if action.HasResponseExample {
		// (*<response type>, *http.Response, error)
		statement.Parens(
			Op("*").Qual(qualifier(endpoint), action.responseTypeName()).Op(",").Op("*").Qual("net/http", "Response").Op(",").Error(),
		)
	} else {
		// *http.Response, error
		statement.Parens(
			Op("*").Qual("net/http", "Response").Op(",").Error(),
		)
	}

	// function body
	statement.Block(
		// u := fmt.Sprintf("%s/<key>", s.path)
		Id("u").Op(":=").Qual("fmt", "Sprintf").Call(
			Lit(fmt.Sprintf("%%s/%s", action.Key)),
			Id("s").Dot("path"),
		),

		ifTrueGen(
			action.HasResponseExample,
			// v := new(projects.BulkUpdateKeyResponse)
			Id("v").Op(":=").New(Qual(qualifier(endpoint), action.responseTypeName())),
		),
		Line(),
		// resp, err := s.client.Call("POST", u, r, v)
		Id("resp, err").Op(":=").Id("s").Dot("client").Dot("Call").Call(
			Id("ctx"),
			Lit("POST"),
			Id("u"),
			ifTrueGenOrNil(
				action.HasResponseExample,
				Id("v"),
			),
			Id("r"),
		),

		//if err != nil {
		//	return nil, err
		//}
		ifErrorReturn(
			action.HasResponseExample,
		),
		Line(),
		// return v, nil
		genReturnWithError(action.HasResponseExample, "v"),
	)

	// Spacing
	statement.Line()

	return statement
}

// Outputs:
//
//	if resp.StatusCode >= 300 {
//		if errorResponse, err := ErrorResponseFrom(resp); err != nil {
//			return nil, fmt.Errorf("could not decode error response: %+v", err)
//		} else {
//			return nil, errorResponse
//		}
//	}
func nonHTTP2xxErrorHandling(action Action) *Statement {
	return If().Id("resp").Dot("StatusCode").Op(">=").Lit(300).Block(
		If().Id("errorResponse").Op(",").Id("err").Op(":=").Qual("", "ErrorResponseFrom").Call(
			Id("resp")).Op(";").Id("err").Op("!=").Nil().Block(
			errResult(
				action,
				Qual("fmt", "Errorf").Call(
					Lit("received non 2xx status code (%d), but could not decode error response: %+v"),
					Id("resp").Dot("StatusCode"),
					Id("err"),
				),
			),
		).Else().Block(
			errResult(
				action,
				Id("errorResponse"),
			),
		),
	)
}

func (s *Service) getServiceFunc(action Action, endpoint string) *Statement {
	// start function signature without return type
	comment := fmt.Sprintf("// %s - %s", action.serviceFuncName(), replaceTags(action.Description))
	if action.Since != "" {
		comment += fmt.Sprintf("\n// Since %s", action.Since)
	}
	if action.DeprecatedSince != "" {
		comment += fmt.Sprintf("\n// Deprecated since %s", action.DeprecatedSince)
	}
	if action.ChangeLog != nil && len(action.ChangeLog) > 0 {
		comment += "\n// Changelog:\n//"
		for _, change := range action.ChangeLog {
			comment += fmt.Sprintf("\n//\t%s: %s", change.Version, replaceTags(change.Description))
		}
	}
	statement := Comment(comment).Line()

	// func(s *<service id>) <action id>(r <request type>)
	statement.Func().Parens(Id("s").Op("*").Id(s.Getter())).Id(action.serviceFuncName())
	statement.Params(
		Id("ctx").Qual("context", "Context"),
		Id("r").Qual(qualifier(endpoint), action.requestTypeName()),
		ifTrueGen(action.hasPaging(), Id("p").Qual(qualifier("paging"), "Params")),
	)

	// add return type based on whether we expect a response
	if action.HasResponseExample {
		// (*<response type>, *http.Response, error)
		statement.Parens(
			Op("*").Qual(qualifier(endpoint), action.responseTypeName()).Op(",").Op("*").Qual("net/http", "Response").Op(",").Error(),
		)
	} else {
		// *http.Response, error
		statement.Parens(
			Op("*").Qual("net/http", "Response").Op(",").Error(),
		)
	}

	// function body
	statement.Block(
		// u := fmt.Sprintf("%s/<key>", s.path)
		Id("u").Op(":=").Qual("fmt", "Sprintf").Call(
			Lit(fmt.Sprintf("%%s/%s", action.Key)),
			Id("s").Dot("path"),
		),

		ifTrueGen(
			action.HasResponseExample,
			// v := new(projects.BulkUpdateKeyResponse)
			Id("v").Op(":=").New(Qual(qualifier(endpoint), action.responseTypeName())),
		),
		Line(),
		// resp, err := s.client.Call("GET", u, r, v)
		Id("resp, err").Op(":=").Id("s").Dot("client").Dot("Call").Call(
			Id("ctx"),
			Lit("GET"),
			Id("u"),
			ifTrueGenOrNil(
				action.HasResponseExample,
				Id("v"),
			),
			Id("r"),
			ifTrueGen(action.hasPaging(), Id("p")),
		),

		//if err != nil {
		//	return nil, resp, err
		//}
		ifErrorReturn(
			action.HasResponseExample,
		),
		Line(),
		// return v, nil
		genReturnWithError(action.HasResponseExample, "v"),
	)

	// Spacing
	statement.Line()

	return statement
}

func (s *Service) getAllServiceFunc(action Action, endpoint string, field Field) *Statement {
	// start function signature without return type
	// func(s *<service id>) <action id>All(r <request type>)
	statement := Func().Parens(Id("s").Op("*").Id(s.Getter())).Id(action.serviceAllFuncName())
	statement.Params(
		Id("ctx").Qual("context", "Context"),
		Id("r").Qual(qualifier(endpoint), action.requestTypeName()),
	)

	// Just to be safe, check field type
	mapField, ok := field.(*MapField)
	if !ok {
		fmt.Printf("Not generating 'All' handler for %s/%s, only map fields supported, got: %+v\n", s.endpoint(), action.Key, reflect.TypeOf(field))
		return Empty()
	}

	// Create an update statement for all fields of the response structure
	accessors := mapField.Accessors()
	updateStatements := make([]Code, len(accessors))
	for i, accessor := range accessors {
		switch mapField.fields[i].(type) {
		case *SliceField:
			// response.<accessor> = append(response.<accessor>, res.<accessor>...)
			updateStatements[i] = Id("response").Dot(accessor).Op("=").Id("append").Call(
				Id("response").Dot(accessor),
				Id("res").Dot(accessor).Op("..."),
			)
		default:
			fmt.Printf("Skipping field '%s' for %s.%s, only slices are supported.\n", mapField.fields[i].Name(), action.Key, action.responseAllTypeName())
		}
	}

	// Paged requests always have a response
	// (*<response type>, error)
	statement.Parens(
		Op("*").Qual(qualifier(endpoint), action.responseAllTypeName()).Op(",").Error(),
	)

	// function body
	funcBody := &Statement{}

	//	p := paging.Params{
	//		P:  1,
	//		Ps: 100,
	//	}
	funcBody.Add(
		Id("p").Op(":=").Qual(qualifier("paging"), "Params").Values(Dict{
			Id("P"):  Lit(1),
			Id("Ps"): Lit(100),
		}),

		Id("response").Op(":=").Op("&").Qual(qualifier(endpoint), action.responseAllTypeName()).Block(),
	)

	loopBody := &Statement{}
	loopBody.Add(
		//	res, _, err := s.<action id>(r, p)
		//	if err != nil {
		//		return nil, fmt.Errorf("error during <action id>All: %+v", err)
		//	}
		Id("res").Op(",").Id("_").Op(",").Err().Op(":=").Id("s").Dot(action.serviceFuncName()).Call(Id("ctx"), Id("r"), Id("p")),
		ifError(action, fmt.Sprintf("error during call to %s.%s: %%+v", endpoint, action.serviceFuncName())),
	)

	// Add update statements for each accessor
	loopBody.Add(updateStatements...)

	loopBody.Add(
		//	if res.Paging.End() {
		//		break
		//	} else {
		//		p.P++
		//	}
		If(Id("res").Dot(action.pagingFuncName()).Call().Dot("End").Call()).Block(
			Break(),
		),
		Id("p").Dot("P").Op("++"),
	)

	//	for {
	funcBody.Add(
		For(nil).Block(*loopBody...))
	//	}

	funcBody.Add(
		// return response, nil
		Return(Id("response"), Nil()),
	)

	statement.Block(*funcBody...)

	// Spacing
	statement.Line()

	return statement
}
