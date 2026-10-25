package main

import (
	"bufio"
	"bytes"
	"encoding/json/v2"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

const ansiBlue = "\\[\033[34m\\]"
const ansiPink = "\\[\033[35m\\]"
const ansiWhite = "\\[\033[97m\\]"
const ansiReset = "\\[\033[0m\\]"

const initScript = `
_lambda_prompt() {
    local exit_code=$?
    PS1="$(lambda-prompt)"
    return "$exit_code"
}
if [[ "${BASH_VERSINFO[0]}" -ge 5 && "${BASH_VERSINFO[1]}" -ge 1 ]]; then
    if [[ ! " ${PROMPT_COMMAND[*]} " =~ " _lambda_prompt " ]]; then
        PROMPT_COMMAND+=(_lambda_prompt)
    fi
else
    if [[ ! ";${PROMPT_COMMAND};" =~ ";_lambda_prompt;" ]]; then
        PROMPT_COMMAND="${PROMPT_COMMAND:+$PROMPT_COMMAND;}_lambda_prompt"
    fi
fi
`

type GitPromptInfo struct {
	RefName        string
	ModifiedFiles  uint64
	UntrackedFiles uint64
	StagedFiles    uint64
	UnmergedFiles  uint64
	StashCount     uint64
	AheadRemote    uint64
	BehindRemote   uint64
}

type JJPromptInfo struct {
	Bookmarks      []string `json:"bookmarks"`
	ChangeIDPrefix string   `json:"change_id_prefix"`
	ModifiedFiles  uint64   `json:"modified_files"`
	UntrackedFiles uint64   `json:"untracked_files"`
}

func getPythonVenv() (string, bool) {
	envVar, present := os.LookupEnv("VIRTUAL_ENV")
	if !present {
		return "", false
	}
	split := strings.Split(envVar, "/")
	if len(split) == 0 {
		return "", false
	}
	return split[len(split)-1], true
}

func getJJPromptInfo() (*JJPromptInfo, error) {
	// Outsource as much work as we
	// can to jj because we can do it
	// in a single command.
	const jjTemplate = `json({
		'change_id_prefix' => change_id.shortest().prefix(),
		'bookmarks' => bookmarks.map(|b| b.name()),
		'modified_files' => self.diff().files().filter(|f| 
			f.status() == 'modified' || f.status() == 'removed' || f.status() == 'renamed'
		).len(),
		'untracked_files' => self.diff().files().filter(|f| 
			f.status() == 'added' || f.status() == 'copied'
		).len()
	})`
	cmd, err := exec.Command("jj", "log", "--no-graph", "-r", "@", "--color=never", "-T", jjTemplate).Output()
	if err != nil {
		return nil, err
	}

	var info JJPromptInfo
	if err := json.Unmarshal(cmd, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func getGitPromptInfo() (*GitPromptInfo, error) {
	cmd := exec.Command("git", "--no-optional-locks", "status", "--porcelain=v2", "--branch", "--show-stash")
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
		case strings.HasPrefix(line, "# branch.oid "):
			if oid := strings.TrimPrefix(line, "# branch.oid "); len(oid) >= 7 {
				if oid == "(initial)" {
					info.RefName = "(initial)"
				} else {
					info.RefName = "@" + oid[:7]
				}
			} else {
				info.RefName = "@" + oid
			}
		case strings.HasPrefix(line, "# branch.head "):
			if branch := strings.TrimPrefix(line, "# branch.head "); branch != "(detached)" {
				info.RefName = branch
			}
		case strings.HasPrefix(line, "# stash "):
			stash := strings.TrimPrefix(line, "# stash ")
			if count, err := strconv.ParseUint(stash, 10, 64); err == nil {
				info.StashCount = count
			}
		case strings.HasPrefix(line, "# branch.ab "):
			aheadBehind := strings.TrimPrefix(line, "# branch.ab ")
			if aheadStr, behindStr, ok := strings.Cut(aheadBehind, " "); ok {
				if len(aheadStr) > 1 {
					if aheadBy, err := strconv.ParseUint(aheadStr, 10, 64); err == nil {
						info.AheadRemote = aheadBy
					}
				}
				if len(behindStr) > 1 {
					if behindBy, err := strconv.ParseUint(behindStr, 10, 64); err == nil {
						info.BehindRemote = behindBy
					}
				}
			}

		case line[0] == '?':
			info.UntrackedFiles++
		case line[0] == 'u':
			info.UnmergedFiles++
		case (line[0] == '1' || line[0] == '2') && len(line) >= 4:
			x := line[2]
			y := line[3]
			if x != '.' {
				info.StagedFiles++
			}
			if y != '.' {
				info.ModifiedFiles++
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return info, nil
}

func shellEscape(s string) string {
	r := strings.NewReplacer(
		`\`, `\\`,
		`$`, `\$`,
		"`", "\\`",
	)
	return r.Replace(s)
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

	if cwd == home || strings.HasPrefix(cwd, home+"/") {
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
		runes := []rune(path)
		if i == len(paths)-1 {
			shortPaths[i] = path
		} else if strings.HasPrefix(path, ".") && len(runes) > 3 {
			shortPaths[i] = string([]rune(path)[:3])
		} else if !strings.HasPrefix(path, ".") && len(runes) > 2 {
			shortPaths[i] = string([]rune(path)[:2])
		} else {
			shortPaths[i] = path
		}
	}
	return strings.Join(shortPaths, "/")
}

func main() {
	if len(os.Args) < 2 {
		var prompt strings.Builder
		prompt.WriteRune(' ')
		if envName, present := getPythonVenv(); present {
			prompt.WriteRune('(')
			prompt.WriteString(envName)
			prompt.WriteString(") ")
		}
		prompt.WriteString(ansiBlue)
		prompt.WriteString(shellEscape(reducePathWidth(getCwd())))
		if jjInfo, err := getJJPromptInfo(); err == nil {
			prompt.WriteString(" " + ansiPink)
			prompt.WriteString(shellEscape(jjInfo.ChangeIDPrefix))
			if len(jjInfo.Bookmarks) > 0 {
				prompt.WriteString(ansiWhite + " (")
				prompt.WriteString(shellEscape(strings.Join(jjInfo.Bookmarks, " ")))
				prompt.WriteString(")" + ansiPink)
			}
			if jjInfo.ModifiedFiles > 0 {
				prompt.WriteString(" " + ansiWhite + "!")
				prompt.WriteString(strconv.FormatUint(jjInfo.ModifiedFiles, 10))
			}
			if jjInfo.UntrackedFiles > 0 {
				if jjInfo.ModifiedFiles > 0 {
					prompt.WriteString(" " + ansiPink + "?")
				} else {
					prompt.WriteString(" ?")
				}
				prompt.WriteString(strconv.FormatUint(jjInfo.UntrackedFiles, 10))
			}
		} else if gitInfo, err := getGitPromptInfo(); err == nil {
			prompt.WriteString(" " + ansiPink)
			prompt.WriteString(shellEscape(gitInfo.RefName))
			if gitInfo.ModifiedFiles > 0 {
				prompt.WriteString(" " + ansiWhite + "!")
				prompt.WriteString(strconv.FormatUint(gitInfo.ModifiedFiles, 10))
			}
			if gitInfo.UntrackedFiles > 0 {
				if gitInfo.ModifiedFiles > 0 {
					prompt.WriteString(" " + ansiPink + "?")
				} else {
					prompt.WriteString(" ?")
				}
				prompt.WriteString(strconv.FormatUint(gitInfo.UntrackedFiles, 10))
			}
			if gitInfo.UnmergedFiles > 0 {
				prompt.WriteString(" " + ansiBlue + "~")
				prompt.WriteString(strconv.FormatUint(gitInfo.UnmergedFiles, 10))
			}
			if gitInfo.StagedFiles > 0 {
				if gitInfo.ModifiedFiles > 0 || gitInfo.UnmergedFiles > 0 {
					prompt.WriteString(" " + ansiPink + "+")
				} else {
					prompt.WriteString(" +")
				}
				prompt.WriteString(strconv.FormatUint(gitInfo.StagedFiles, 10))
			}
			if gitInfo.StashCount > 0 {
				if gitInfo.ModifiedFiles > 0 || gitInfo.UnmergedFiles > 0 {
					prompt.WriteString(" " + ansiPink + "*")
				} else {
					prompt.WriteString(" *")
				}
				prompt.WriteString(strconv.FormatUint(gitInfo.StashCount, 10))
			}
			if gitInfo.AheadRemote > 0 {
				prompt.WriteString(" " + ansiWhite + "a")
				prompt.WriteString(strconv.FormatUint(gitInfo.AheadRemote, 10))
			}
			if gitInfo.BehindRemote > 0 {
				if gitInfo.ModifiedFiles > 0 || gitInfo.UnmergedFiles > 0 || gitInfo.AheadRemote > 0 {
					prompt.WriteString(" " + ansiPink + "b")
				} else {
					prompt.WriteString(" b")
				}
				prompt.WriteString(strconv.FormatUint(gitInfo.BehindRemote, 10))
			}
		}
		prompt.WriteString(" " + ansiBlue + "λ" + ansiReset + " ")
		fmt.Print(prompt.String())
		return
	}
	switch os.Args[1] {
	case "init":
		fmt.Println(initScript)
	default:
		fmt.Println("Unknown command")
	}
}
