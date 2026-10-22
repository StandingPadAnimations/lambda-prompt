package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const ansiBlue = "\\[\033[34m\\]"
const ansiPink = "\\[\033[35m\\]"
const ansiWhite = "\\[\033[97m\\]"
const ansiReset = "\\[\033[0m\\]"

const initScript = `
function _lambda_prompt() {
	PS1="$(lambda-prompt)"
}
PROMPT_COMMAND=_lambda_prompt
`

func gitInfo() (string, error) {
	cmd := exec.Command("git", "symbolic-ref", "--short", "HEAD")
	cmd.Env = append(cmd.Environ(), "LANG=en")

	out, err := cmd.Output()
	if err != nil {
		return "", err
	}

	branch := strings.TrimSpace(string(out))
	if branch == "HEAD" {
		hashCmd := exec.Command("git", "rev-parse", "--short", "HEAD")
		if hashOut, err := hashCmd.Output(); err == nil {
			branch = ":" + strings.TrimSpace(string(hashOut))
		}
	}

	return branch, nil
}

func getCwd() string {
	cwd, err := os.Getwd()
	if err != nil {
		cwd = "CAN'T GET CURRENT WORKING DIRECTORY"
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return cwd
	}

	if strings.HasPrefix(cwd, home) {
		cwd = strings.Replace(cwd, home, "~", 1)
	}
	return cwd
}

func main() {
	if len(os.Args) < 2 {
		prompt := fmt.Sprintf(" %sλ %s", ansiBlue, getCwd())
		if gitBranch, err := gitInfo(); err == nil && gitBranch != "" {
			prompt = fmt.Sprintf("%s %s⑂ %s", prompt, ansiPink, gitBranch)
		}
		prompt = fmt.Sprintf("%s%s ", prompt, ansiReset)
		fmt.Println(prompt)
		return
	}
	switch os.Args[1] {
	case "init":
		fmt.Println(initScript)
	default:
		fmt.Println("Unknown command")
	}
}
