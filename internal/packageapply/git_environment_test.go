package packageapply

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	count := 0
	if raw := os.Getenv("GIT_CONFIG_COUNT"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			panic(err)
		}
		count = parsed
	}
	index := strconv.Itoa(count)
	if err := os.Setenv("GIT_CONFIG_KEY_"+index, "core.autocrlf"); err != nil {
		panic(err)
	}
	if err := os.Setenv("GIT_CONFIG_VALUE_"+index, "false"); err != nil {
		panic(err)
	}
	if err := os.Setenv("GIT_CONFIG_COUNT", strconv.Itoa(count+1)); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func TestGitFixtureLineEndingsAreDeterministic(t *testing.T) {
	out, err := exec.Command("git", "config", "--get", "core.autocrlf").CombinedOutput()
	if err != nil {
		t.Fatalf("git config core.autocrlf: %v\n%s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != "false" {
		t.Fatalf("core.autocrlf=%q want=false", got)
	}
}
