// Package lever provides a simple interface to access configuration
// parameters, accessed through either command line, environment variables, or
// configuration file.
//
// Initializing
//
// An instance is initialized like so:
//
//	f := lever.New("myapp", nil)
//
//	// -- or - can be used, or really anything, or nothing! Check the Name field
//	// doc for more specific rules
//	f.Add(lever.Param{Name: "--foo"})
//	f.Add(lever.Param{Name: "--bar", Default: "something"})
//	f.Add(lever.Param{Name: "--foo-bar", Flag: true})
//	f.Parse()
//
// "myapp" is the name of the application using lever, and nil could be a set
// of Opts if you want, but nil uses the default values which is probably fine.
//
// In addition to the given Params, lever automatically adds in "--help" (to
// print out a help page and exit), "--example" (to print out an example config
// file and exit) and "--config" (to read in a config file and use values from
// it). Exact behaviour can be tweaked through both Param and Opt fields.
//
// Values for the set params can be passed in through either the command line,
// environment variables, a config file, or all three. The order of precedence
// goes:
//
// - command line (highest)
//
// - environment variables
//
// - config file
//
// - default value
//
// Using command line
//
// For the above app, command line options could be set like (Note that both the
// "--key val" and "--key=val" forms are valid):
//
//	./myapp --foo foo --bar=bar --foo-bar
//
// Using environment variables
//
// For the above app, environment variables could be set like (Note the
// prepending of the app's name):
//
//	export MYAPP_FOO=foo
//	export MYAPP_FOO_BAR=true
//	./myapp
//
// Using a config file
//
// To see an example configuration file for your app, use the "--example"
// command line flag. It will print and example config file with all default
// values already filled in to stdout.
//
//	./myapp --example > myapp.conf
//	./myapp --config myapp.conf
//
// Retrieving values
//
// Any of the Param* methods can be used to retrieve values from within your
// app. For the app above you could values with something like:
//
//	foo, ok := f.ParamInt("--foo")
//	bar, ok := f.ParamStr("--bar")
//	foobar, ok := f.ParamFlag("--foo-bar")
//
// You use these methods regardless of the source of the values (command line,
// environment, etc...). The names will be automatically translated from their
// source naming to match what was used for the Name field in the original
// Param.
//
// Multi params
//
// Multi params are those which can be specified more than once. You don't have
// to specify a Param in a special way, only retrieve it in a special way:
//
//	f := lever.New("myapp2", nil)
//	f.Add(lever.Param{Name: "--multi"})
//	f.Parse
//
//	// ./myapp2 --multi foo --multi bar
//
//	m, ok := f.ParamStrs("--multi")
//	// m == []string{"foo", "bar"}
//
// You can set the default value for multi params as well, using the
// DefaultMulti field. If one source sets *any* values for the multi param all
// values from sources of lower priority are thrown away, for example:
//
//	export MYAPP2_MULTI=foo
//	./myapp2 --multi bar --multi baz
//	# multi == []string{"bar", "baz"}
//
package lever

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Param is a single configuration option which is specified by the user running
// the application
type Param struct {

	// Required. This the long form of the flag name. It should include any
	// delimiters which wish to be used, for example "--long-form" or
	// "-long-form". Name cannot contain :, =, or whitespace
	Name string

	// Other names for the same parameter which will be accepted on the command
	// line, generally used for short form flags. Like Name these should include
	// any delimiters which wish to be used, for example "-alias" or "-s"
	Aliases []string

	// A short description of the paramater, shown in command-line help and as a
	// comment in the default configuration file
	Description string

	// The default value this param should take, as a string. If this param is
	// going to be parsed as something else, like an int, the default string
	// should also be parsable as an int.
	//
	// If this param is a flag this can be "", "false" (equivalent), or "true"
	Default string

	// If the param is going to be used as a type which is specified multiple
	// times (for example, using ParamStrs()), this is the default value field
	// which should be used. nil means to refer to Default, empty slice means
	// the default is no entries
	DefaultMulti []string

	// If the param is a flag (boolean switch, it doesn't expect a value), this
	// must be set to true
	Flag bool

	// If set to true this parameter will not appear in the example
	// configuration file and will not be allowed to be set in it
	DisallowInConfigFile bool
}

// configName returns the name the param will take in the config file
func (p *Param) configName() string {
	return strings.TrimLeftFunc(p.Name, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
}

func (p *Param) flagDefault() bool {
	return p.Default == "true"
}

// Implement sort.Interface
type params []*Param

func (ps params) Len() int {
	return len(ps)
}

func (ps params) Less(i, j int) bool {
	return ps[i].Name < ps[j].Name
}

func (ps params) Swap(i, j int) {
	ps[i], ps[j] = ps[j], ps[i]
}

// Opts are options which can be used when instantiating a new instance of a
// Lever to change its behavior. None of them are required to be set
type Opts struct {

	// If set lever will look in the given file (if it exists) for
	DefaultConfigFile string

	// Extra text which will be shown above the output of Help() when --help is
	// set. A newline is not required
	HelpHeader string

	// Extra text which will be shown below the output of Help() when --help is
	// set. A newline is not required
	HelpFooter string

	// Don't allow there to be a configuration file imported. the --config and
	// --example cli options won't be present in the output of Help()
	DisallowConfigFile bool

	// If the config file is missing even though it is specified (either through
	// default value or on the command line) do not error
	AllowMissingConfigFile bool
}

// Lever is an instance of the paramater parser, which can have expected
// parameters assigned to it and their values retrieved from it
type Lever struct {
	appName      string
	o            *Opts
	expected     map[string]*Param
	expectedFull map[string]*Param // expected, plus another key for each alias
	found        map[string][]string
	remaining    []string
}

// New instantiates a new Lever with the given appName and Opts (or nil to just
// use the default Opts). appName is used as the name of the application using
// lever, both in the example configuration file and as a prefix to environment
// variables
func New(appName string, o *Opts) *Lever {
	if o == nil {
		o = &Opts{}
	}

	f := Lever{
		appName:      appName,
		o:            o,
		expected:     map[string]*Param{},
		expectedFull: map[string]*Param{},
	}

	if !o.DisallowConfigFile {
		f.Add(Param{
			Name:                 "--config",
			Aliases:              []string{"-config"},
			Description:          "Configuration file to load",
			Default:              o.DefaultConfigFile,
			DisallowInConfigFile: true,
		})

		f.Add(Param{
			Name:                 "--example",
			Aliases:              []string{"-example"},
			Description:          "Dump an example configuration, filled with default values, to stdout",
			Flag:                 true,
			DisallowInConfigFile: true,
		})
	}

	f.Add(Param{
		Name:                 "--help",
		Aliases:              []string{"-help", "-h"},
		Description:          "Print this help message",
		Flag:                 true,
		DisallowInConfigFile: true,
	})

	return &f
}

// Add the given parameter as an expected parameter for the process
func (f *Lever) Add(p Param) {
	f.expected[p.Name] = &p
	f.expectedFull[p.Name] = &p
	for _, alias := range p.Aliases {
		f.expectedFull[alias] = &p
	}
}

func (f *Lever) sortedExpected() params {
	ps := make(params, 0, len(f.expected))
	for _, p := range f.expected {
		ps = append(ps, p)
	}
	sort.Sort(ps)
	return ps
}

// Help returns a string representing exactly what would be written to stdout if
// --help is set
func (f *Lever) Help() string {
	buf := bytes.NewBuffer(make([]byte, 0, 1024))
	ps := f.sortedExpected()

	if f.o.HelpHeader != "" {
		fmt.Fprintf(buf, f.o.HelpHeader)
	}

	fmt.Fprintln(buf, "")
	for _, p := range ps {
		fmt.Fprintf(buf, "\t%s", p.Name)
		for _, alias := range p.Aliases {
			fmt.Fprintf(buf, ", %s", alias)
		}
		if p.Flag {
			fmt.Fprintf(buf, " (flag)")
		}
		fmt.Fprintf(buf, "\n")

		var multiline bool
		if p.Description != "" {
			fmt.Fprintf(buf, "\t\t%s\n", p.Description)
			multiline = true
		}

		if p.DefaultMulti != nil {
			fmt.Fprintf(buf, "\t\tDefault: %v\n", p.DefaultMulti)
			multiline = true
		} else if p.Default != "" {
			fmt.Fprintf(buf, "\t\tDefault: %s\n", p.Default)
			multiline = true
		}

		// Add another newline if the parameter spanned multiple lines
		if multiline {
			fmt.Fprintf(buf, "\n")
		}
	}

	if f.o.HelpFooter != "" {
		fmt.Fprintln(buf, f.o.HelpFooter)
	}

	return buf.String()
}

// Example returns a string representing exactly what would be written to stdout
// if --example is set
func (f *Lever) Example() string {
	buf := bytes.NewBuffer(make([]byte, 0, 1024))
	ps := f.sortedExpected()

	fmt.Fprintf(buf, "# %s configuration\n\n", f.appName)
	for _, p := range ps {
		if p.DisallowInConfigFile {
			continue
		}

		if p.Description != "" {
			fmt.Fprintf(buf, "# %s\n", p.Description)
		}

		name := p.configName()
		if p.Flag {
			if p.flagDefault() {
				fmt.Fprintf(buf, "%s: true\n", name)
			} else {
				fmt.Fprintf(buf, "%s: false\n", name)
			}
		} else if len(p.DefaultMulti) > 0 {
			for _, d := range p.DefaultMulti {
				fmt.Fprintf(buf, "%s: %s\n", name, d)
			}
		} else if p.DefaultMulti != nil {
			fmt.Fprintf(buf, "# %s:\n", name)
		} else {
			fmt.Fprintf(buf, "%s: %s\n", name, p.Default)
		}
		fmt.Fprintf(buf, "\n")
	}

	return buf.String()
}

// readCLI takes in the given args, presumably from the cli (minus the call
// string) and parses them in the context of the expected parameters
func (f *Lever) readCLI(args []string) (map[string][]string, []string) {
	var arg string
	found := map[string][]string{}
	remaining := make([]string, 0, len(args))

	for {
		if len(args) == 0 {
			return found, remaining
		}

		arg, args = args[0], args[1:]
		if arg == "--" {
			remaining = append(remaining, args...)
			return found, remaining
		}

		argParts := strings.SplitN(arg, "=", 2)
		argName := argParts[0]
		var argVal string
		var argValOk bool

		if len(argParts) == 2 {
			argVal = argParts[1]
			argValOk = true
		}

		p, ok := f.expectedFull[argName]
		if !ok {
			remaining = append(remaining, arg)
			continue
		}

		if p.Flag {
			if p.flagDefault() {
				found[p.Name] = append(found[p.Name], "false")
			} else {
				found[p.Name] = append(found[p.Name], "true")
			}
			continue
		}

		if !argValOk && len(args) > 0 {
			argVal, args = args[0], args[1:]
		}

		found[p.Name] = append(found[p.Name], argVal)
	}
}

// readConfig reads any expected params out of the given reader and returns the
// ones found, or an error if something goes wrong
func (f *Lever) readConfig(r io.Reader) (map[string][]string, error) {
	found := map[string][]string{}
	rr := bufio.NewReader(r)

	for {
		line, err := rr.ReadString('\n')
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}

		line = strings.TrimSpace(line)
		if line == "" || line[0] == '#' {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("could not parse line: %q", line)
		}

		name, val := parts[0], strings.TrimSpace(parts[1])
		for n := range f.expected {
			if !strings.HasSuffix(n, name) {
				continue
			}
			found[n] = append(found[n], val)
		}
	}

	return found, nil
}

// Will attempt to find and read the config file based on the expected
// parameters (namely the default config file) and the found parameter values so
// far. It will return the config file's found values, or nil if no config file
// is specified
func (f *Lever) maybeReadConfig(
	found map[string][]string,
) (
	map[string][]string, error,
) {
	var fn string
	if c, ok := found["--config"]; ok && c[0] != "" {
		fn = c[0]
	} else if def := f.expected["--config"].Default; def != "" {
		fn = def
	} else {
		return nil, nil
	}

	fd, err := os.Open(fn)
	if err != nil {
		if f.o.AllowMissingConfigFile && os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("error opening %s: %s", fn, err)
	}

	foundConfig, err := f.readConfig(fd)
	if err != nil {
		return nil, fmt.Errorf("error reading %s: %s", fn, err)
	}
	return foundConfig, nil
}

// formats a strings as a standard environment variable, changing all characters
// to uppercase and switching - for _
func envify(s string) string {
	s = strings.ToUpper(s)
	return strings.Replace(s, "-", "_", -1)
}

// readEnv reads any expected params out of the given environment (each element
// of the form key=val) and returns the ones found, or an error if something
// goes wrong
func (f *Lever) readEnv(environ []string) map[string][]string {
	appNameEnv := envify(f.appName)
	expectedEnv := map[string]*Param{}

	for _, p := range f.expected {
		n := appNameEnv + "_" + envify(p.configName())
		expectedEnv[n] = p
	}

	found := map[string][]string{}
	for _, env := range environ {
		parts := strings.SplitN(env, "=", 2)
		name, val := parts[0], parts[1]
		if p, ok := expectedEnv[name]; ok {
			found[p.Name] = append(found[p.Name], val)
		}
	}

	return found
}

// defaultsAsFound returns all the expected param's default values as if they
// were pulled from a real source (like cli or config file). This is used in the
// final part of Parse(), to fill out any values which weren't specified
// anywhere
func (f *Lever) defaultsAsFound() map[string][]string {
	found := map[string][]string{}
	for n, p := range f.expected {
		if p.DefaultMulti != nil {
			found[n] = p.DefaultMulti
		} else if p.Default != "" {
			found[n] = []string{p.Default}
		}
	}
	return found
}

func mergeFound(into, from map[string][]string) {
	for n, p := range from {
		if _, ok := into[n]; !ok {
			into[n] = p
		}
	}
}

// Parse looks at all available sources of param values (command line,
// environment variables, configuration file) and puts together all the
// discovered values. Once this returns it is possible to retrieve values for
// specific params. If the --help or --example flags are set on the command line
// their associated output is dumped to stdout os.Exit(0) will be called.
func (f *Lever) Parse() {
	found, remaining := f.readCLI(os.Args[1:])

	if help, ok := found["--help"]; ok && help[0] == "true" {
		fmt.Fprint(os.Stdout, f.Help())
		os.Stdout.Sync()
		os.Exit(0)
	}

	if example, ok := found["--example"]; ok && example[0] == "true" {
		fmt.Fprint(os.Stdout, f.Example())
		os.Stdout.Sync()
		os.Exit(0)
	}

	foundEnv := f.readEnv(os.Environ())
	mergeFound(found, foundEnv)

	if !f.o.DisallowConfigFile {
		foundConfig, err := f.maybeReadConfig(found)
		if err != nil {
			fmt.Fprintln(os.Stderr, err.Error())
			os.Stderr.Sync()
			os.Exit(1)
		}
		mergeFound(found, foundConfig)
	}

	foundDef := f.defaultsAsFound()
	mergeFound(found, foundDef)

	f.found = found
	f.remaining = remaining
}

// paramSingleStr returns the set value of the param as if it was only set once
// (whether or not it actually was), along with whether or not it was actually
// found
func (f *Lever) paramSingleStr(name string) (string, bool) {
	if vs, ok := f.found[name]; ok {
		return vs[0], true
	}
	return "", false
}

// ParamStr returns the value of the param of the given name as a string. True
// is returned if the value is set by either the user or a default value
func (f *Lever) ParamStr(name string) (string, bool) {
	return f.paramSingleStr(name)
}

// ParamStrs returns all the values of the param (if it was set multiple times)
// of the given name as strings. True is returned if the values were set by
// either the user or the default values
func (f *Lever) ParamStrs(name string) ([]string, bool) {
	vs, ok := f.found[name]
	if vs == nil {
		vs = []string{}
	}
	return vs, ok
}

// ParamInt returns the value of the param of the given name as an int. True is
// returned if the value was set by either the user or a default value
func (f *Lever) ParamInt(name string) (int, bool) {
	v, ok := f.paramSingleStr(name)
	if !ok {
		return 0, false
	}

	i, err := strconv.Atoi(v)
	if err != nil {
		return 0, false
	}

	return i, true
}

// ParamInts returns all the values of the param (if it was set multiple times)
// of the given name as ints. True is returned if the values were set by either
// the user or the default values
func (f *Lever) ParamInts(name string) ([]int, bool) {
	vs, ok := f.ParamStrs(name)
	if !ok {
		return []int{}, false
	}

	is := make([]int, len(vs))
	for ii := range vs {
		i, err := strconv.Atoi(vs[ii])
		if err != nil {
			return []int{}, false
		}
		is[ii] = i
	}

	return is, true
}

// ParamFlag returns the value of the param of the given name as a boolean. The
// param need only be set with no value to take on the opposite value of its
// Default. This almost always means that if the flag is set true is returned.
func (f *Lever) ParamFlag(name string) bool {
	v, _ := f.paramSingleStr(name)
	p, ok := f.expectedFull[name]
	if !ok {
		// This is kind of weird but whatever, do something kind of sane
		return v != ""
	}
	def := p.flagDefault()
	if v == "" || v == "false" {
		return def
	}
	return !def
}

// ParamRest returns any command line parameters which were passed in by the
// user but not expected. In addition, any paramaters following a "--" parameter
// on the command line will automatically be appended to this list regardless of
// their value
func (f *Lever) ParamRest() []string {
	return f.remaining
}
