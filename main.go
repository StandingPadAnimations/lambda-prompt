package main

import (
	"bufio"
	"bytes"
	"encoding/json/v2"
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
_lambda_prompt() {
    local exit=$?
    PS1="$(lambda-prompt)"
    return $exit
}
if [[ ! " ${PROMPT_COMMAND[*]} " =~ " _lambda_prompt " ]]; then
    PROMPT_COMMAND+=(_lambda_prompt)
fi
`

type GitPromptInfo struct {
	RefName        string
	ModifiedFiles  uint64
	UntrackedFiles uint64
}

type JJPromptInfo struct {
	Bookmarks      []string `json:"bookmarks"`
	ChangeIDPrefix string   `json:"change_id_prefix"`
	ModifiedFiles  uint64   `json:"modified_files"`
	UntrackedFiles uint64   `json:"untracked_files"`
}

func getJJPromptInfo() (*JJPromptInfo, error) {
	// Outsource as much work to jj
	// because we can do it in a single
	// command.
	cmd, err := exec.Command("jj", "log", "--no-graph", "-r", "@", "--color=never", "-T", "json({'change_id_prefix' => change_id.shortest().prefix(), 'bookmarks' => bookmarks.map(|b| b.name()), 'modified_files' => self.diff().files().filter(|f| f.status() == 'modified' || f.status() == 'removed' || f.status() == 'renamed').len(), 'untracked_files' => self.diff().files().filter(|f| f.status() == 'added' || f.status() == 'copied').len()})").Output()
	if err != nil {
		return nil, err
	}
	jjInfo := strings.TrimSpace(string(cmd))

	var info JJPromptInfo
	if err := json.Unmarshal([]byte(jjInfo), &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func getGitPromptInfo() (*GitPromptInfo, error) {
	cmd := exec.Command("git", "status", "--porcelain=v2", "--branch")
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	info := &GitPromptInfo{}
	scanner := bufio.NewScanner(bytes.NewReader(out))

	for scanner.Scan() {
		line := scanner.Text()
		if len(line) == 0 {
			continue
		}

		switch {
		case strings.HasPrefix(line, "# branch.head "):
			branch := strings.TrimPrefix(line, "# branch.head ")
			if branch == "(detached)" {
				info.RefName = "detached"
			} else {
				info.RefName = branch
			}
		case strings.HasPrefix(line, "# branch.oid ") && info.RefName == "detached":
			oid := strings.TrimPrefix(line, "# branch.oid ")
			if len(oid) >= 7 {
				info.RefName = oid[:7]
			} else {
				info.RefName = oid
			}
		case line[0] == '?':
			info.UntrackedFiles++
		case line[0] == '1' || line[0] == '2' || line[0] == 'u':
			info.ModifiedFiles++
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return info, nil
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

func reducePathWidth(path string) string {
	if len(path) < 30 {
		return path
	}
	paths := strings.Split(path, "/")
	shortPaths := make([]string, len(paths))
	for i, path := range paths {
		if (len(path) < 2) || (i == len(paths)-1) {
			shortPaths[i] = path
		} else {
			if strings.HasPrefix(path, ".") {
				shortPaths[i] = path[:3]
			} else {
				shortPaths[i] = path[:2]
			}
		}
	}
	return strings.Join(shortPaths, "/")
}

func main() {
	if len(os.Args) < 2 {
		var prompt strings.Builder
		prompt.WriteRune(' ')
		prompt.WriteString(ansiBlue)
		prompt.WriteString(reducePathWidth(getCwd()))
		refIndicator := ""
		changedIndicator := ""
		untrackedIndicator := ""
		if jjInfo, err := getJJPromptInfo(); err == nil {
			refIndicator = jjInfo.ChangeIDPrefix
			if jjInfo.ModifiedFiles > 0 {
				changedIndicator = fmt.Sprintf("!%d", jjInfo.ModifiedFiles)
			}
			if jjInfo.UntrackedFiles > 0 {
				untrackedIndicator = fmt.Sprintf("?%d", jjInfo.UntrackedFiles)
			}
			if len(jjInfo.Bookmarks) > 0 {
				refIndicator = fmt.Sprintf("%s %s(%s)%s", refIndicator, ansiWhite, strings.Join(jjInfo.Bookmarks, " "), ansiPink)
			}
		} else if gitInfo, err := getGitPromptInfo(); err == nil {
			refIndicator = gitInfo.RefName
			if gitInfo.ModifiedFiles > 0 {
				changedIndicator = fmt.Sprintf("!%d", gitInfo.ModifiedFiles)
			}
			if gitInfo.UntrackedFiles > 0 {
				untrackedIndicator = fmt.Sprintf("?%d", gitInfo.UntrackedFiles)
			}
		}
		if refIndicator != "" {
			prompt.WriteRune(' ')
			prompt.WriteString(ansiPink)
			prompt.WriteString(refIndicator)

			// Concatenate these to avoid large
			// amounts of spaces
			if changedIndicator != "" {
				prompt.WriteRune(' ')
				prompt.WriteString(changedIndicator)
			}
			if untrackedIndicator != "" {
				prompt.WriteRune(' ')
				prompt.WriteString(untrackedIndicator)
			}
		}
		prompt.WriteRune(' ')
		prompt.WriteString(ansiBlue)
		prompt.WriteRune('λ')
		prompt.WriteString(ansiReset)
		prompt.WriteRune(' ')
		fmt.Println(prompt.String())
		return
	}
	switch os.Args[1] {
	case "init":
		fmt.Println(initScript)
	default:
		fmt.Println("Unknown command")
	}
}
