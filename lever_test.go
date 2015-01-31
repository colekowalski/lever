package lever

import (
	"bytes"
	"fmt"
	. "testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testLever(disallowConfig bool) *Lever {
	f := New("test-app", &Opts{
		DisallowConfigFile: disallowConfig,
	})
	f.Add(Param{Name: "--foo"})
	f.Add(Param{Name: "--flag1", Flag: true})
	f.Add(Param{Name: "--bar", Aliases: []string{"-b"}, Description: "wut"})
	f.Add(Param{Name: "--baz", Aliases: []string{"-c"}, Description: "wut", Default: "wat"})
	f.Add(Param{Name: "--flag2", Flag: true})
	f.Add(Param{
		Name:         "--buz",
		Aliases:      []string{"-d"},
		Description:  "wut",
		DefaultMulti: []string{"a", "b", "c"},
	})
	f.Add(Param{Name: "--byz", DefaultMulti: []string{}})
	return f
}

func TestHelp(t *T) {
	f := testLever(false)
	assert.Equal(t, `
	--bar, -b
		wut

	--baz, -c
		wut
		Default: wat

	--buz, -d
		wut
		Default: [a b c]

	--byz
		Default: []

	--config, -config
		Configuration file to load

	--example, -example (flag)
		Dump an example configuration, filled with default values, to stdout

	--flag1 (flag)
	--flag2 (flag)
	--foo
	--help, -help, -h (flag)
		Print this help message

`, f.Help())

	f = testLever(true)
	assert.Equal(t, `
	--bar, -b
		wut

	--baz, -c
		wut
		Default: wat

	--buz, -d
		wut
		Default: [a b c]

	--byz
		Default: []

	--flag1 (flag)
	--flag2 (flag)
	--foo
	--help, -help, -h (flag)
		Print this help message

`, f.Help())

	f.o.HelpHeader = "header"
	f.o.HelpFooter = "footer"
	assert.Equal(t, `header
	--bar, -b
		wut

	--baz, -c
		wut
		Default: wat

	--buz, -d
		wut
		Default: [a b c]

	--byz
		Default: []

	--flag1 (flag)
	--flag2 (flag)
	--foo
	--help, -help, -h (flag)
		Print this help message

footer
`, f.Help())
}

func TestExample(t *T) {
	f := testLever(false)
	assert.Equal(t, fmt.Sprintf(`# test-app configuration

# wut
bar: 

# wut
baz: wat

# wut
buz: a
buz: b
buz: c

# byz:

flag1: false

flag2: false

foo: 

`), f.Example())
}

func TestReadCLI(t *T) {
	f := testLever(false)
	found, remaining := f.readCLI([]string{
		"--bar=butts", "-c", "baz", "--buz=buz", "--buz", "buz2", "--flag1",
		"something", "--unk=wat", "--foo",
	})

	assert.Equal(t, map[string][]string{
		"--bar":   []string{"butts"},
		"--baz":   []string{"baz"},
		"--buz":   []string{"buz", "buz2"},
		"--flag1": []string{"true"},
		"--foo":   []string{""},
	}, found)
	assert.Equal(t, []string{"something", "--unk=wat"}, remaining)

	found, remaining = f.readCLI([]string{
		"--bar=butts", "-c", "baz", "--buz=buz", "--", "--buz", "buz2",
		"--flag1", "something", "--unk=wat", "--foo",
	})

	assert.Equal(t, map[string][]string{
		"--bar": []string{"butts"},
		"--baz": []string{"baz"},
		"--buz": []string{"buz"},
	}, found)
	assert.Equal(t, []string{
		"--buz", "buz2", "--flag1", "something", "--unk=wat", "--foo",
	}, remaining)
}

func TestReadConfig(t *T) {
	f := testLever(false)
	b := bytes.NewBuffer(make([]byte, 0, 1024))
	fmt.Fprintf(b, `
bar: wat

#this is a comment
# this is also
	# so is this
####
	
foo:
buz: a
buz: b
flag1: true

unk: ok
buz: c
`)

	found, err := f.readConfig(b)
	require.Nil(t, err)
	assert.Equal(t, map[string][]string{
		"--bar":   []string{"wat"},
		"--foo":   []string{""},
		"--buz":   []string{"a", "b", "c"},
		"--flag1": []string{"true"},
	}, found)
}

func TestReadEnv(t *T) {
	f := testLever(false)
	f.Add(Param{Name: "--foo-bar"})

	env := []string{
		"TEST_APP_BAR=bar",
		"TEST_APP_FLAG1=true",
		"HOME=whatever",
		"TEST_APP_FOO_BAR=okthen",
	}

	assert.Equal(t, map[string][]string{
		"--bar":     []string{"bar"},
		"--flag1":   []string{"true"},
		"--foo-bar": []string{"okthen"},
	}, f.readEnv(env))
}
