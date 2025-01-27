package viewrt

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"strings"

	"github.com/livebud/bud/framework/view/ssr"
	"github.com/livebud/bud/package/budhttp"
	"github.com/livebud/bud/package/js"
	"github.com/livebud/bud/package/log"
)

type Server interface {
	Middleware(http.Handler) http.Handler
	Handler(route string, props interface{}) http.Handler
}

func Proxy(client budhttp.Client, log log.Interface) *liveServer {
	return &liveServer{http.FS(client), log, &renderer{client, client}}
}

type liveServer struct {
	hfs      http.FileSystem
	log      log.Interface
	renderer *renderer
}

var _ Server = (*liveServer)(nil)

func (s *liveServer) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isClient(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		file, err := s.hfs.Open(strings.TrimPrefix(r.URL.Path, "/"))
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			s.log.Error("view: open error", "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer file.Close()
		stat, err := file.Stat()
		if err != nil {
			s.log.Error("view: stat error", "error", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Maintain support to resolve and run "/bud/node_modules/livebud/runtime".
		if strings.HasPrefix(r.URL.Path, "/bud/node_modules/") ||
			strings.HasSuffix(r.URL.Path, ".svelte") {
			w.Header().Set("Content-Type", "application/javascript")
		}
		http.ServeContent(w, r, r.URL.Path, stat.ModTime(), file)
	})
}

func (s *liveServer) Handler(route string, props interface{}) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.respond(w, route, props)
	})
}

// Respond is a convenience function for render
func (s *liveServer) respond(w http.ResponseWriter, path string, props interface{}) {
	res, err := s.render(path, props)
	if err != nil {
		s.log.Error("view: render error", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	headers := w.Header()
	for key, value := range res.Headers {
		headers.Set(key, value)
	}
	w.WriteHeader(res.Status)
	w.Write([]byte(res.Body))
}

func (s *liveServer) render(route string, props interface{}) (*ssr.Response, error) {
	return s.renderer.Render(route, props)
}

// Static server serves the same files every time. Used during production.
func Static(fsys fs.FS, log log.Interface, vm js.VM, wrapProps func(path string, props interface{}) interface{}) *staticServer {
	return &staticServer{http.FS(fsys), log, &renderer{fsys, vm}}
}

type staticServer struct {
	hfs      http.FileSystem
	log      log.Interface
	renderer *renderer
}

var _ Server = (*staticServer)(nil)

// Map is a convenience function for the common case of passing a map of props
// into a view
type Map map[string]interface{}

// Respond is a convenience function for render
func (s *staticServer) respond(w http.ResponseWriter, path string, props interface{}) {
	res, err := s.render(path, props)
	if err != nil {
		s.log.Error("view: client open error", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	headers := w.Header()
	for key, value := range res.Headers {
		headers.Set(key, value)
	}
	w.WriteHeader(res.Status)
	w.Write([]byte(res.Body))
}

func (s *staticServer) render(path string, props interface{}) (*ssr.Response, error) {
	return s.renderer.Render(path, props)
}

func isClient(path string) bool {
	return strings.HasPrefix(path, "/bud/node_modules/") ||
		strings.HasPrefix(path, "/bud/view/")
}

func (s *staticServer) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isClient(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		s.serveHTTP(w, r)
	})
}

// Handler returns a handler for a specific server-side route
func (s *staticServer) Handler(route string, props interface{}) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.respond(w, route, props)
	})
}

func (s *staticServer) serveHTTP(w http.ResponseWriter, r *http.Request) {
	file, err := s.hfs.Open(r.URL.Path)
	if err != nil {
		s.log.Error("view: open error", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	stat, err := file.Stat()
	if err != nil {
		s.log.Error("view: stat error", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Maintain support to resolve and run "/bud/node_modules/livebud/runtime".
	if strings.HasPrefix(r.URL.Path, "/bud/node_modules/") {
		w.Header().Add("Content-Type", "text/javascript")
	}
	http.ServeContent(w, r, r.URL.Path, stat.ModTime(), file)
}

type renderer struct {
	fsys fs.FS
	vm   js.VM
}

func (r *renderer) Render(route string, props interface{}) (*ssr.Response, error) {
	propBytes, err := json.Marshal(props)
	if err != nil {
		return nil, err
	}
	script, err := fs.ReadFile(r.fsys, "bud/view/_ssr.js")
	if err != nil {
		return nil, err
	}
	// Evaluate the server
	expr := fmt.Sprintf(`%s; bud.render(%q, %s)`, script, route, propBytes)
	result, err := r.vm.Eval("_ssr.js", expr)
	if err != nil {
		return nil, err
	}
	// Unmarshal the response
	res := new(ssr.Response)
	if err := json.Unmarshal([]byte(result), res); err != nil {
		return nil, err
	}
	if res.Status < 100 || res.Status > 999 {
		return nil, fmt.Errorf("view: invalid status code %d", res.Status)
	}
	return res, nil
}
