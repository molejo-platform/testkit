package main

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/graphql-go/graphql"
)

var graphQLSchema = newGraphQLSchema()

func graphQL(writer http.ResponseWriter, request *http.Request, logger *slog.Logger) {
	if request.Method != http.MethodPost {
		writer.Header().Set("Allow", http.MethodPost)
		http.Error(writer, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var payload struct {
		Query         string                 `json:"query"`
		Variables     map[string]interface{} `json:"variables"`
		OperationName string                 `json:"operationName"`
	}
	if err := decodeJSONBody(writer, request, maxGraphQLBodyBytes, &payload); err != nil {
		return
	}
	if strings.TrimSpace(payload.Query) == "" {
		http.Error(writer, "GraphQL query is required", http.StatusBadRequest)
		return
	}
	started := time.Now()
	result := graphql.Do(graphql.Params{
		Schema: graphQLSchema, RequestString: payload.Query, VariableValues: payload.Variables,
		OperationName: payload.OperationName, Context: request.Context(),
	})
	level, status := slog.LevelInfo, "success"
	if len(result.Errors) > 0 {
		level, status = slog.LevelWarn, "failed"
	}
	logger.LogAttrs(request.Context(), level, "GraphQL operation completed",
		slog.String("event", "graphql.operation.completed"),
		slog.String("route", "/graphql"),
		slog.String("status", status),
		slog.Int("error_count", len(result.Errors)),
		slog.Float64("duration_ms", durationMilliseconds(time.Since(started))),
		slog.String("correlation_id", request.Header.Get(correlationHeader)),
	)
	writeJSON(writer, http.StatusOK, result)
}

func newGraphQLSchema() graphql.Schema {
	query := graphql.NewObject(graphql.ObjectConfig{Name: "Query", Fields: graphql.Fields{
		"status":  &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(graphql.ResolveParams) (interface{}, error) { return "ok", nil }},
		"version": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Resolve: func(graphql.ResolveParams) (interface{}, error) { return version, nil }},
		"echo": &graphql.Field{Type: graphql.NewNonNull(graphql.String), Args: graphql.FieldConfigArgument{
			"message": &graphql.ArgumentConfig{Type: graphql.NewNonNull(graphql.String)},
		}, Resolve: func(params graphql.ResolveParams) (interface{}, error) {
			message, ok := params.Args["message"].(string)
			if !ok {
				return nil, errors.New("message must be a string")
			}
			return message, nil
		}},
	}})
	schema, err := graphql.NewSchema(graphql.SchemaConfig{Query: query})
	if err != nil {
		panic(err)
	}
	return schema
}
