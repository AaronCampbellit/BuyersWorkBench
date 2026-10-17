package httpapi

import (
	"net/http"
	"reflect"
	"strings"
	"time"

	"buyersworkbench/internal/domain"
	"buyersworkbench/internal/workbench"
)

// Route metadata is registered alongside each actual handler, so the published
// route catalog cannot advertise an unregistered implementation.
func (s *Server) route(mux *http.ServeMux, pattern string, handler http.HandlerFunc) {
	mux.HandleFunc(pattern, handler)
	parts := strings.SplitN(pattern, " ", 2)
	if len(parts) != 2 {
		return
	}
	method, path := strings.ToLower(parts[0]), parts[1]
	if s.operations[path] == nil {
		s.operations[path] = map[string]any{}
	}
	operation := map[string]any{"responses": map[string]any{"200": map[string]any{"description": "Successful read or update; response shapes are defined under components/schemas."}, "401": map[string]any{"description": "Authentication required."}, "404": map[string]any{"description": "Missing or unauthorized resource."}}}
	if path != "/api/login" && path != "/healthz" && path != "/readyz" {
		operation["security"] = []any{map[string]any{"sessionCookie": []any{}}}
	}
	parameters := []any{}
	for _, segment := range strings.Split(path, "/") {
		if strings.HasPrefix(segment, "{") {
			parameters = append(parameters, map[string]any{"name": strings.Trim(segment, "{}"), "in": "path", "required": true, "schema": map[string]any{"type": "string"}})
		}
	}
	if method == "post" || method == "patch" {
		parameters = append(parameters, map[string]any{"name": "Origin", "in": "header", "required": true, "schema": map[string]any{"type": "string"}})
		if path != "/api/login" {
			parameters = append(parameters, map[string]any{"name": "X-CSRF-Token", "in": "header", "required": true, "schema": map[string]any{"type": "string"}})
		}
		contentType := "application/json"
		schema := map[string]any{"type": "object"}
		if strings.HasSuffix(path, "/imports/internal-orders/preview") {
			contentType = "text/csv"
			schema = map[string]any{"type": "string", "description": "UTF-8 CSV, maximum 1 MiB and 1000 rows; obtain the template route."}
		}
		operation["requestBody"] = map[string]any{"required": true, "content": map[string]any{contentType: map[string]any{"schema": schema}}}
		operation["responses"] = map[string]any{"200": map[string]any{"description": "Updated or idempotent result."}, "201": map[string]any{"description": "Created preview or note."}, "202": map[string]any{"description": "Accepted durable collection. Poll the returned run ID."}, "204": map[string]any{"description": "Completed without a response body."}, "403": map[string]any{"description": "Origin, CSRF, or first-use password check failed."}, "409": map[string]any{"description": "Stale generation/revision or incompatible idempotency key."}, "422": map[string]any{"description": "Invalid domain inputs."}}
	}
	if len(parameters) > 0 {
		operation["parameters"] = parameters
	}
	s.operations[path][method] = operation
}
func contractSchema(t reflect.Type) map[string]any {
	if t == reflect.TypeFor[time.Time]() {
		return map[string]any{"type": "string", "format": "date-time"}
	}
	if t.Kind() == reflect.Pointer {
		return map[string]any{"anyOf": []any{contractSchema(t.Elem()), map[string]any{"type": "null"}}}
	}
	switch t.Kind() {
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int64, reflect.Int32:
		return map[string]any{"type": "integer"}
	case reflect.Slice:
		return map[string]any{"type": "array", "items": contractSchema(t.Elem())}
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": contractSchema(t.Elem())}
	case reflect.Struct:
		properties := map[string]any{}
		required := []string{}
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if !field.IsExported() {
				continue
			}
			tag := strings.Split(field.Tag.Get("json"), ",")
			if tag[0] == "-" {
				continue
			}
			if field.Anonymous && tag[0] == "" {
				embedded := contractSchema(field.Type)
				for k, v := range embedded["properties"].(map[string]any) {
					properties[k] = v
				}
				required = append(required, embedded["required"].([]string)...)
				continue
			}
			name := tag[0]
			if name == "" {
				name = field.Name
			}
			properties[name] = contractSchema(field.Type)
			if len(tag) < 2 || tag[1] != "omitempty" {
				required = append(required, name)
			}
		}
		return map[string]any{"type": "object", "properties": properties, "required": required}
	default:
		return map[string]any{}
	}
}
func (s *Server) openAPI(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authenticate(w, r); !ok {
		return
	}
	models := map[string]any{"User": User{}, "Workspace": Workspace{}, "Overview": workbench.Overview{}, "OrderDetail": workbench.OrderDetail{}, "Run": workbench.Run{}, "WorkflowInput": workbench.WorkflowInput{}, "Exception": workbench.Exception{}, "Note": workbench.Note{}, "Quotes": workbench.Quotes{}, "Quote": domain.Quote{}, "Requirement": domain.Requirement{}, "ResetResult": workbench.ResetResult{}, "Supplier": workbench.Supplier{}, "SupplierInput": workbench.SupplierInput{}}
	schemas := map[string]any{}
	models["PurchaseOrder"] = workbench.PurchaseOrder{}
	models["PurchaseOrderInput"] = domain.PurchaseOrderInput{}
	models["PurchaseOrderActionInput"] = domain.PurchaseOrderActionInput{}
	models["PurchaseOrderDetail"] = workbench.PurchaseOrderDetail{}
	models["Connection"] = workbench.Connection{}
	models["ConnectionInput"] = workbench.ConnectionInput{}
	models["NotificationPreferences"] = workbench.NotificationPreferences{}
	models["WorkspaceMember"] = workbench.WorkspaceMember{}
	for name, model := range models {
		schemas[name] = contractSchema(reflect.TypeOf(model))
	}
	respond(w, 200, map[string]any{"openapi": "3.1.0", "info": map[string]any{"title": "BuyersWorkBench API", "version": "1.0.0", "description": "Runtime-generated handler catalog and Go response schemas. See docs/API.md for validated mutation input fields and bounded-list semantics. Decimal values are strings. Live Lenovo collection is deferred."}, "paths": s.operations, "components": map[string]any{"schemas": schemas, "securitySchemes": map[string]any{"sessionCookie": map[string]any{"type": "apiKey", "in": "cookie", "name": cookieName}}}})
}
