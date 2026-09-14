package config

import "testing"

func TestParseVariablesReadsEveryVariable(t *testing.T) {
	vars, err := ParseVariables([]byte("[submodule \"Lib\"]\n\tURL = https://example.com/lib\n\tpath\n"))

	if err != nil || len(vars) != 2 {
		t.Fatalf("vars = %+v, %v", vars, err)
	}
	if vars[0].Name() != "submodule.Lib.url" || vars[0].Value != "https://example.com/lib" || vars[1].Key != "path" || vars[1].HasValue {
		t.Fatalf("vars = %+v", vars)
	}
}

func TestParseVariablesKeepsTheVariablesReadBeforeAnError(t *testing.T) {
	vars, err := ParseVariables([]byte("[submodule \"a\"]\n\turl = -x\ngarbage[\n[b]\n\tc = d\n"))

	if err == nil || len(vars) != 1 || vars[0].Name() != "submodule.a.url" {
		t.Fatalf("vars = %+v, %v", vars, err)
	}
}
