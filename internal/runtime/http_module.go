package runtime

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ArubikU/polyloft-bvm/internal/value"
)

type VMProxy interface {
	CallClosure(closure value.Value, args []value.Value) (value.Value, error)
	CallClosureIsolated(closure value.Value, args []value.Value) (value.Value, error)
	StringifyValue(candidate value.Value) (string, error)
	TypeOfValue(candidate value.Value) string
	InstanceOfValue(candidate value.Value, target value.Value) (bool, error)
}

var GlobalVMProxy VMProxy

func BuildHttpModule() *RuntimeModule {
	builder := NewModuleBuilder("Http")

	// Http client binding
	// polyloft.http.native.client_request(method, url, body, headers, timeoutMillis)
	builder.AddTypedFunction("client_request", []string{TypeString, TypeString, TypeString, TypeMap, TypeInt}, TypeMap, false, func(args []value.Value) (value.Value, error) {
		method := args[0].Str
		urlStr := args[1].Str
		reqBody := args[2].Str
		headersObj, _ := args[3].AsMap()
		timeout := int(args[4].Num)

		client := &http.Client{
			Timeout: time.Duration(timeout) * time.Millisecond,
		}

		var bodyReader io.Reader
		if reqBody != "" {
			bodyReader = bytes.NewBufferString(reqBody)
		}

		req, err := http.NewRequest(method, urlStr, bodyReader)
		if err != nil {
			return value.NilValue(), fmt.Errorf("failed to create http request: %w", err)
		}

		if headersObj != nil {
			for k, vVal := range headersObj.Entries {
				if vVal.Kind == value.String {
					req.Header.Add(k, vVal.Str)
				}
			}
		}

		resp, err := client.Do(req)
		if err != nil {
			return value.NilValue(), fmt.Errorf("http request failed: %w", err)
		}
		defer resp.Body.Close()

		respBodyBytes, _ := io.ReadAll(resp.Body)
		respBody := string(respBodyBytes)

		respHeaders := make(map[string]value.Value)
		for k, vals := range resp.Header {
			if len(vals) > 0 {
				respHeaders[k] = value.StringValue(vals[0])
			}
		}

		resultMap := make(map[string]value.Value)
		resultMap["status"] = value.IntValue(int64(resp.StatusCode))
		resultMap["body"] = value.StringValue(respBody)
		resultMap["headers"] = value.ObjectValue(&value.Map{Entries: respHeaders})

		return value.ObjectValue(&value.Map{Entries: resultMap}), nil
	})

	// polyloft.http.native.server_listen(port, callbackFunction)
	// Server loops synchronously, waiting for a stop signal internally or blocking forever.
	// Since Polyloft VM is single threaded, we don't spin a hidden Go routine that freely calls `vm.Call`.
	// Instead, the builtin locks the VM context, serving requests.
	builder.AddTypedFunction("server_listen", []string{TypeInt, TypeFunction}, TypeVoid, false, func(args []value.Value) (value.Value, error) {
		port := int(args[0].Num)
		callback := args[1]

		if GlobalVMProxy == nil {
			return value.NilValue(), fmt.Errorf("VM context is missing for HTTP Server callback invocation")
		}

		mux := http.NewServeMux()
		mux.HandleFunc("/", func(w http.ResponseWriter, req *http.Request) {
			// Convert req to map
			reqHeaders := make(map[string]value.Value)
			for k, vals := range req.Header {
				if len(vals) > 0 {
					reqHeaders[k] = value.StringValue(vals[0])
				}
			}

			bodyBytes, _ := io.ReadAll(req.Body)
			reqObjMap := make(map[string]value.Value)
			reqObjMap["method"] = value.StringValue(req.Method)
			reqObjMap["url"] = value.StringValue(req.URL.String())
			reqObjMap["path"] = value.StringValue(req.URL.Path)
			reqObjMap["body"] = value.StringValue(string(bodyBytes))
			reqObjMap["headers"] = value.ObjectValue(&value.Map{Entries: reqHeaders})

			polyloftReq := value.ObjectValue(&value.Map{Entries: reqObjMap})

			// Synchronously invoke via Proxy
			res, err := GlobalVMProxy.CallClosure(callback, []value.Value{polyloftReq})
			if err != nil {
				w.WriteHeader(http.StatusInternalServerError)
				fmt.Fprintf(w, "Polyloft Error: %v\n", err)
				return
			}

			// Parse expected `{ status: int, body: string, headers: map }` response map
			if resMap, ok := res.AsMap(); ok {
				status := 200
				body := ""

				if st, ok := resMap.Entries["status"]; ok && st.Kind == value.Number {
					status = int(st.Num)
				}
				if bd, ok := resMap.Entries["body"]; ok && bd.Kind == value.String {
					body = bd.Str
				}
				if hds, ok := resMap.Entries["headers"]; ok {
					if hm, isMap := hds.AsMap(); isMap {
						for hk, hv := range hm.Entries {
							w.Header().Add(hk, hv.String())
						}
					}
				}

				w.WriteHeader(status)
				w.Write([]byte(body))
			} else {
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(res.String()))
			}
		})

		server := &http.Server{
			Addr:    fmt.Sprintf(":%d", port),
			Handler: mux,
		}

		// Since Polyloft is single-threaded, and the callback execution expects a clean stack without race conditions,
		// we MUST lock during execution, or we just rely on `proxy.CallClosure` internally locking with `sync.Mutex` inside the VM.
		// Polyloft `vm.Call()` modifies the VM's frame stack directly.
		err := server.ListenAndServe()
		if err != nil {
			return value.NilValue(), fmt.Errorf("server error: %w", err)
		}

		return value.NilValue(), nil
	})

	return builder.Build()
}
