// Copyright 2017 Google Inc. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package driver

import (
	"bytes"
	"fmt"
	"html/template"
	"net"
	"net/http"
	gourl "net/url"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"pproflame/internal/graph"
	"pproflame/internal/plugin"
	"pproflame/internal/report"
	"pproflame/profile"

	"github.com/gin-gonic/gin"
)

// WebInterface holds the state needed for serving a browser based interface.
type WebInterface struct {
	prof      *profile.Profile
	options   *plugin.Options
	help      map[string]string
	templates *template.Template
}

// MakeWebInterface 获取 Web UI 对象
func MakeWebInterface(p *profile.Profile, opt *plugin.Options) *WebInterface {
	templates := template.New("templategroup")
	addTemplates(templates)
	report.AddSourceTemplates(templates)
	return &WebInterface{
		prof:      p,
		options:   opt,
		help:      make(map[string]string),
		templates: templates,
	}
}

// maxEntries is the maximum number of entries to print for text interfaces.
const maxEntries = 50

// errorCatcher is a UI that captures errors for reporting to the browser.
type errorCatcher struct {
	plugin.UI
	errors []string
}

func (ec *errorCatcher) PrintErr(args ...interface{}) {
	ec.errors = append(ec.errors, strings.TrimSuffix(fmt.Sprintln(args...), "\n"))
	ec.UI.PrintErr(args...)
}

// webArgs contains arguments passed to templates in webhtml.go.
type webArgs struct {
	Title      string
	Errors     []string
	Total      int64
	Legend     []string
	Help       map[string]string
	Nodes      []string
	HTMLBody   template.HTML
	TextBody   string
	Top        []report.TextItem
	FlameGraph template.JS
}

func serveWebInterface(hostport string, p *profile.Profile, o *plugin.Options) error {
	host, port, err := getHostAndPort(hostport)
	if err != nil {
		return err
	}
	interactiveMode = true
	ui := MakeWebInterface(p, o)
	for n, c := range PProfCommands {
		ui.help[n] = c.description
	}
	for n, v := range PProfVariables {
		ui.help[n] = v.help
	}
	ui.help["details"] = "Show information about the profile and this view"
	ui.help["graph"] = "Display profile as a directed graph"
	ui.help["reset"] = "Show the entire profile"

	server := o.HTTPServer
	if server == nil {
		server = defaultWebServer
	}
	args := &plugin.HTTPServerArgs{
		Hostport: net.JoinHostPort(host, strconv.Itoa(port)),
		Host:     host,
		Port:     port,
		Handlers: map[string]http.Handler{
		// "/":           http.HandlerFunc(ui.Dot),
		// "/top":        http.HandlerFunc(ui.Top),
		// "/disasm":     http.HandlerFunc(ui.Disasm),
		// "/source":     http.HandlerFunc(ui.Source),
		// "/peek":       http.HandlerFunc(ui.Peek),
		// "/flamegraph": http.HandlerFunc(ui.Flamegraph),
		},
	}

	if o.UI.WantBrowser() {
		go openBrowser("http://"+args.Hostport, o)
	}
	return server(args)
}

func getHostAndPort(hostport string) (string, int, error) {
	host, portStr, err := net.SplitHostPort(hostport)
	if err != nil {
		return "", 0, fmt.Errorf("could not split http address: %v", err)
	}
	if host == "" {
		host = "localhost"
	}
	var port int
	if portStr == "" {
		ln, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
		if err != nil {
			return "", 0, fmt.Errorf("could not generate random port: %v", err)
		}
		port = ln.Addr().(*net.TCPAddr).Port
		err = ln.Close()
		if err != nil {
			return "", 0, fmt.Errorf("could not generate random port: %v", err)
		}
	} else {
		port, err = strconv.Atoi(portStr)
		if err != nil {
			return "", 0, fmt.Errorf("invalid port number: %v", err)
		}
	}
	return host, port, nil
}
func defaultWebServer(args *plugin.HTTPServerArgs) error {
	ln, err := net.Listen("tcp", args.Hostport)
	if err != nil {
		return err
	}
	isLocal := isLocalhost(args.Host)
	handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if isLocal {
			// Only allow local clients
			host, _, err := net.SplitHostPort(req.RemoteAddr)
			if err != nil || !isLocalhost(host) {
				http.Error(w, "permission denied", http.StatusForbidden)
				return
			}
		}
		h := args.Handlers[req.URL.Path]
		if h == nil {
			// Fall back to default behavior
			h = http.DefaultServeMux
		}
		h.ServeHTTP(w, req)
	})

	// We serve the ui at /ui/ and redirect there from the root. This is done
	// to surface any problems with serving the ui at a non-root early. See:
	//
	// https://github.com/google/pprof/pull/348
	mux := http.NewServeMux()
	mux.Handle("/ui/", http.StripPrefix("/ui", handler))
	mux.Handle("/", http.RedirectHandler("/ui/", http.StatusTemporaryRedirect))
	s := &http.Server{Handler: mux}
	return s.Serve(ln)
}

func isLocalhost(host string) bool {
	for _, v := range []string{"localhost", "127.0.0.1", "[::1]", "::1"} {
		if host == v {
			return true
		}
	}
	return false
}

func openBrowser(url string, o *plugin.Options) {
	// Construct URL.
	u, _ := gourl.Parse(url)
	q := u.Query()
	for _, p := range []struct{ param, key string }{
		{"f", "focus"},
		{"s", "show"},
		{"i", "ignore"},
		{"h", "hide"},
	} {
		if v := PProfVariables[p.key].value; v != "" {
			q.Set(p.param, v)
		}
	}
	u.RawQuery = q.Encode()

	// Give server a little time to get ready.
	time.Sleep(time.Millisecond * 500)

	for _, b := range browsers() {
		args := strings.Split(b, " ")
		if len(args) == 0 {
			continue
		}
		viewer := exec.Command(args[0], append(args[1:], u.String())...)
		viewer.Stderr = os.Stderr
		if err := viewer.Start(); err == nil {
			return
		}
	}
	// No visualizer succeeded, so just print URL.
	o.UI.PrintErr(u.String())
}

func varsFromURL(u *gourl.URL) variables {
	vars := PProfVariables.makeCopy()
	vars["focus"].value = u.Query().Get("f")
	vars["show"].value = u.Query().Get("s")
	vars["ignore"].value = u.Query().Get("i")
	vars["hide"].value = u.Query().Get("h")
	return vars
}

// makeReport generates a report for the specified command.
func (ui *WebInterface) makeReport(c *gin.Context,
	cmd []string, vars ...string) (*report.Report, []string) {
	v := varsFromURL(c.Request.URL)
	for i := 0; i+1 < len(vars); i += 2 {
		v[vars[i]].value = vars[i+1]
	}
	catcher := &errorCatcher{UI: ui.options.UI}
	options := *ui.options
	options.UI = catcher
	_, rpt, err := generateRawReport(ui.prof, cmd, v, &options)
	if err != nil {
		c.String(http.StatusBadRequest, err.Error())
		ui.options.UI.PrintErr(err)
		return nil, nil
	}
	return rpt, catcher.errors
}

// render generates html using the named template based on the contents of data.
func (ui *WebInterface) render(c *gin.Context, tmpl string,
	rpt *report.Report, errList, legend []string, data webArgs) {
	file := getFromLegend(legend, "File: ", "unknown")
	profile := getFromLegend(legend, "Type: ", "unknown")
	data.Title = file + " " + profile
	data.Errors = errList
	data.Total = rpt.Total()
	data.Legend = legend
	data.Help = ui.help
	html := &bytes.Buffer{}
	if err := ui.templates.ExecuteTemplate(html, tmpl, data); err != nil {
		c.String(http.StatusInternalServerError, "internal template error")
		ui.options.UI.PrintErr(err)
		return
	}
	c.Header("Content-Type", "text/html")
	c.Writer.Write(html.Bytes())
}

// Dot generates a web page containing an svg diagram.
func (ui *WebInterface) Dot(c *gin.Context) {
	rpt, errList := ui.makeReport(c, []string{"svg"})
	if rpt == nil {
		return // error already reported
	}

	// Generate dot graph.
	g, config := report.GetDOT(rpt)
	legend := config.Labels
	config.Labels = nil
	dot := &bytes.Buffer{}
	graph.ComposeDot(dot, g, &graph.DotAttributes{}, config)

	// Convert to svg.
	svg, err := dotToSvg(dot.Bytes())
	if err != nil {
		c.String(http.StatusNotImplemented, "Could not execute dot; may need to install graphviz.")
		ui.options.UI.PrintErr("Failed to execute dot. Is Graphviz installed?\n", err)
		return
	}

	// Get all node names into an array.
	nodes := []string{""} // dot starts with node numbered 1
	for _, n := range g.Nodes {
		nodes = append(nodes, n.Info.Name)
	}

	ui.render(c, "graph", rpt, errList, legend, webArgs{
		HTMLBody: template.HTML(string(svg)),
		Nodes:    nodes,
	})
}

func dotToSvg(dot []byte) ([]byte, error) {
	cmd := exec.Command("dot", "-Tsvg")
	out := &bytes.Buffer{}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = bytes.NewBuffer(dot), out, os.Stderr
	if err := cmd.Run(); err != nil {
		return nil, err
	}

	// Fix dot bug related to unquoted amperands.
	svg := bytes.Replace(out.Bytes(), []byte("&;"), []byte("&amp;;"), -1)

	// Cleanup for embedding by dropping stuff before the <svg> start.
	if pos := bytes.Index(svg, []byte("<svg")); pos >= 0 {
		svg = svg[pos:]
	}
	return svg, nil
}

// Top  top
func (ui *WebInterface) Top(c *gin.Context) {
	rpt, errList := ui.makeReport(c, []string{"top"}, "nodecount", "500")
	if rpt == nil {
		return // error already reported
	}
	top, legend := report.TextItems(rpt)
	var nodes []string
	for _, item := range top {
		nodes = append(nodes, item.Name)
	}

	ui.render(c, "top", rpt, errList, legend, webArgs{
		Top:   top,
		Nodes: nodes,
	})
}

// Disasm generates a web page containing disassembly.
func (ui *WebInterface) Disasm(c *gin.Context) {
	args := []string{"disasm", c.Request.URL.Query().Get("f")}
	rpt, errList := ui.makeReport(c, args)
	if rpt == nil {
		return // error already reported
	}

	out := &bytes.Buffer{}
	if err := report.PrintAssembly(out, rpt, ui.options.Obj, maxEntries); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		ui.options.UI.PrintErr(err)
		return
	}

	legend := report.ProfileLabels(rpt)
	ui.render(c, "plaintext", rpt, errList, legend, webArgs{
		TextBody: out.String(),
	})

}

// Source generates a web page containing source code annotated with profile
// data.
func (ui *WebInterface) Source(c *gin.Context) {
	args := []string{"weblist", c.Request.URL.Query().Get("f")}
	rpt, errList := ui.makeReport(c, args)
	if rpt == nil {
		return // error already reported
	}

	// Generate source listing.
	var body bytes.Buffer
	if err := report.PrintWebList(&body, rpt, ui.options.Obj, maxEntries); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		ui.options.UI.PrintErr(err)
		return
	}

	legend := report.ProfileLabels(rpt)
	ui.render(c, "sourcelisting", rpt, errList, legend, webArgs{
		HTMLBody: template.HTML(body.String()),
	})
}

// Peek generates a web page listing callers/callers.
func (ui *WebInterface) Peek(c *gin.Context) {
	args := []string{"peek", c.Request.URL.Query().Get("f")}
	rpt, errList := ui.makeReport(c, args, "lines", "t")
	if rpt == nil {
		return // error already reported
	}

	out := &bytes.Buffer{}
	if err := report.Generate(out, rpt, ui.options.Obj); err != nil {
		c.String(http.StatusBadRequest, err.Error())
		ui.options.UI.PrintErr(err)
		return
	}

	legend := report.ProfileLabels(rpt)
	ui.render(c, "plaintext", rpt, errList, legend, webArgs{
		TextBody: out.String(),
	})
}

// getFromLegend returns the suffix of an entry in legend that starts
// with param.  It returns def if no such entry is found.
func getFromLegend(legend []string, param, def string) string {
	for _, s := range legend {
		if strings.HasPrefix(s, param) {
			return s[len(param):]
		}
	}
	return def
}
