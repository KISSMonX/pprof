// Copyright 2014 Google Inc. All Rights Reserved.
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

// Package driver implements the core pprof functionality. It can be
// parameterized with a flag implementation, fetch and symbolize
// mechanisms.
package driver

import (
	"bytes"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"pproflame/internal/plugin"
	"pproflame/internal/report"
	"pproflame/profile"
)

// SetDefaults 默认配置
func SetDefaults(option *plugin.Options) *plugin.Options {
	if option == nil {
		log.Panicln("输入的配置为空")
		return option
	}
	return setDefaults(option)
}

// FetchSrcProfiles 拉取指定源的 pprof 数据
func FetchSrcProfiles(option *plugin.Options, source string, httpHostPort string) (*profile.Profile, error) {
	src, cmd, err := parseFlags(option)
	if err != nil {
		return nil, err
	}

	// NOTE: 暂不处理 cmd 命令
	cmd = cmd

	src.HTTPHostport = httpHostPort
	src.Sources = []string{source}

	p, err := fetchProfiles(src, option)
	if err != nil {
		return nil, err
	}

	return p, nil
}

// RenderFetchedProfiles 渲染拉取的 pprof 数据到页面
func RenderFetchedProfiles(p *profile.Profile, o *plugin.Options, wantBrowser bool) error {
	host, port, err := getHostAndPort(hostport)
	if err != nil {
		return err
	}
	interactiveMode = true
	ui := makeWebInterface(p, o)
	for n, c := range pprofCommands {
		ui.help[n] = c.description
	}
	for n, v := range pprofVariables {
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
			"/":           http.HandlerFunc(ui.dot),
			"/top":        http.HandlerFunc(ui.top),
			"/disasm":     http.HandlerFunc(ui.disasm),
			"/source":     http.HandlerFunc(ui.source),
			"/peek":       http.HandlerFunc(ui.peek),
			"/flamegraph": http.HandlerFunc(ui.flamegraph),
		},
	}

	if wantBrowser {
		go openBrowser("http://"+args.Hostport, o)
	}
	return server(args)
}

// PProf acquires a profile, and symbolizes it using a profile
// manager. Then it generates a report formatted according to the
// options selected through the flags package.
func PProf(eo *plugin.Options, source, timeout string, httpHostPort string) error {
	// Remove any temporary files created during pprof processing.
	defer cleanupTempFiles()

	o := setDefaults(eo)

	// TODO: 各个参数要搞明白, 尽量提供原先的参数不改动, 这样方便后期扩展
	src, cmd, err := parseFlags(o)
	if err != nil {
		return err
	}

	p, err := fetchProfiles(src, o)
	if err != nil {
		return err
	}

	if cmd != nil {
		return generateReport(p, cmd, pprofVariables, o)
	}

	if src.HTTPHostport != "" {
		return serveWebInterface(src.HTTPHostport, p, o)
	}
	return interactive(p, o)
}

func generateRawReport(p *profile.Profile, cmd []string, vars variables, o *plugin.Options) (*command, *report.Report, error) {
	p = p.Copy() // Prevent modification to the incoming profile.

	// Identify units of numeric tags in profile.
	numLabelUnits := identifyNumLabelUnits(p, o.UI)

	// Get report output format
	c := pprofCommands[cmd[0]]
	if c == nil {
		panic("unexpected nil command")
	}

	vars = applyCommandOverrides(cmd[0], c.format, vars)

	// Delay focus after configuring report to get percentages on all samples.
	relative := vars["relative_percentages"].boolValue()
	if relative {
		if err := applyFocus(p, numLabelUnits, vars, o.UI); err != nil {
			return nil, nil, err
		}
	}
	ropt, err := reportOptions(p, numLabelUnits, vars)
	if err != nil {
		return nil, nil, err
	}
	ropt.OutputFormat = c.format
	if len(cmd) == 2 {
		s, err := regexp.Compile(cmd[1])
		if err != nil {
			return nil, nil, fmt.Errorf("parsing argument regexp %s: %v", cmd[1], err)
		}
		ropt.Symbol = s
	}

	rpt := report.New(p, ropt)
	if !relative {
		if err := applyFocus(p, numLabelUnits, vars, o.UI); err != nil {
			return nil, nil, err
		}
	}
	if err := aggregate(p, vars); err != nil {
		return nil, nil, err
	}

	return c, rpt, nil
}

func generateReport(p *profile.Profile, cmd []string, vars variables, o *plugin.Options) error {
	c, rpt, err := generateRawReport(p, cmd, vars, o)
	if err != nil {
		return err
	}

	// Generate the report.
	dst := new(bytes.Buffer)
	if err := report.Generate(dst, rpt, o.Obj); err != nil {
		return err
	}
	src := dst

	// If necessary, perform any data post-processing.
	if c.postProcess != nil {
		dst = new(bytes.Buffer)
		if err := c.postProcess(src, dst, o.UI); err != nil {
			return err
		}
		src = dst
	}

	// If no output is specified, use default visualizer.
	output := vars["output"].value
	if output == "" {
		if c.visualizer != nil {
			return c.visualizer(src, os.Stdout, o.UI)
		}
		_, err := src.WriteTo(os.Stdout)
		return err
	}

	// Output to specified file.
	o.UI.PrintErr("Generating report in ", output)
	out, err := o.Writer.Open(output)
	if err != nil {
		return err
	}
	if _, err := src.WriteTo(out); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func applyCommandOverrides(cmd string, outputFormat int, v variables) variables {
	trim, tagfilter, filter := v["trim"].boolValue(), true, true

	switch cmd {
	case "callgrind", "kcachegrind":
		trim = false
		v.set("addresses", "t")
	case "disasm", "weblist":
		trim = false
		v.set("addressnoinlines", "t")
	case "peek":
		trim, tagfilter, filter = false, false, false
	case "list":
		v.set("nodecount", "0")
		v.set("lines", "t")
	case "text", "top", "topproto":
		if v["nodecount"].intValue() == -1 {
			v.set("nodecount", "0")
		}
	default:
		if v["nodecount"].intValue() == -1 {
			v.set("nodecount", "80")
		}
	}

	if outputFormat == report.Proto || outputFormat == report.Raw {
		trim, tagfilter, filter = false, false, false
		v.set("addresses", "t")
	}

	if !trim {
		v.set("nodecount", "0")
		v.set("nodefraction", "0")
		v.set("edgefraction", "0")
	}
	if !tagfilter {
		v.set("tagfocus", "")
		v.set("tagignore", "")
	}
	if !filter {
		v.set("focus", "")
		v.set("ignore", "")
		v.set("hide", "")
		v.set("show", "")
		v.set("show_from", "")
	}
	return v
}

func aggregate(prof *profile.Profile, v variables) error {
	var inlines, function, filename, linenumber, address bool
	switch {
	case v["addresses"].boolValue():
		return nil
	case v["lines"].boolValue():
		inlines = true
		function = true
		filename = true
		linenumber = true
	case v["files"].boolValue():
		inlines = true
		filename = true
	case v["functions"].boolValue():
		inlines = true
		function = true
	case v["noinlines"].boolValue():
		function = true
	case v["addressnoinlines"].boolValue():
		function = true
		filename = true
		linenumber = true
		address = true
	default:
		return fmt.Errorf("unexpected granularity")
	}
	return prof.Aggregate(inlines, function, filename, linenumber, address)
}

func reportOptions(p *profile.Profile, numLabelUnits map[string]string, vars variables) (*report.Options, error) {
	si, mean := vars["sample_index"].value, vars["mean"].boolValue()
	value, meanDiv, sample, err := sampleFormat(p, si, mean)
	if err != nil {
		return nil, err
	}

	stype := sample.Type
	if mean {
		stype = "mean_" + stype
	}

	if vars["divide_by"].floatValue() == 0 {
		return nil, fmt.Errorf("zero divisor specified")
	}

	var filters []string
	for _, k := range []string{"focus", "ignore", "hide", "show", "show_from", "tagfocus", "tagignore", "tagshow", "taghide"} {
		v := vars[k].value
		if v != "" {
			filters = append(filters, k+"="+v)
		}
	}

	ropt := &report.Options{
		CumSort:      vars["cum"].boolValue(),
		CallTree:     vars["call_tree"].boolValue(),
		DropNegative: vars["drop_negative"].boolValue(),

		CompactLabels: vars["compact_labels"].boolValue(),
		Ratio:         1 / vars["divide_by"].floatValue(),

		NodeCount:    vars["nodecount"].intValue(),
		NodeFraction: vars["nodefraction"].floatValue(),
		EdgeFraction: vars["edgefraction"].floatValue(),

		ActiveFilters: filters,
		NumLabelUnits: numLabelUnits,

		SampleValue:       value,
		SampleMeanDivisor: meanDiv,
		SampleType:        stype,
		SampleUnit:        sample.Unit,

		OutputUnit: vars["unit"].value,

		SourcePath: vars["source_path"].stringValue(),
		TrimPath:   vars["trim_path"].stringValue(),
	}

	if len(p.Mapping) > 0 && p.Mapping[0].File != "" {
		ropt.Title = filepath.Base(p.Mapping[0].File)
	}

	return ropt, nil
}

// identifyNumLabelUnits returns a map of numeric label keys to the units
// associated with those keys.
func identifyNumLabelUnits(p *profile.Profile, ui plugin.UI) map[string]string {
	numLabelUnits, ignoredUnits := p.NumLabelUnits()

	// Print errors for tags with multiple units associated with
	// a single key.
	for k, units := range ignoredUnits {
		ui.PrintErr(fmt.Sprintf("For tag %s used unit %s, also encountered unit(s) %s", k, numLabelUnits[k], strings.Join(units, ", ")))
	}
	return numLabelUnits
}

type sampleValueFunc func([]int64) int64

// sampleFormat returns a function to extract values out of a profile.Sample,
// and the type/units of those values.
func sampleFormat(p *profile.Profile, sampleIndex string, mean bool) (value, meanDiv sampleValueFunc, v *profile.ValueType, err error) {
	if len(p.SampleType) == 0 {
		return nil, nil, nil, fmt.Errorf("profile has no samples")
	}
	index, err := p.SampleIndexByName(sampleIndex)
	if err != nil {
		return nil, nil, nil, err
	}
	value = valueExtractor(index)
	if mean {
		meanDiv = valueExtractor(0)
	}
	v = p.SampleType[index]
	return
}

func valueExtractor(ix int) sampleValueFunc {
	return func(v []int64) int64 {
		return v[ix]
	}
}
