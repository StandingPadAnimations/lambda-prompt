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
	jjInfo := strings.TrimSpace(string(cmd))

	var info JJPromptInfo
	if err := json.Unmarshal([]byte(jjInfo), &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func getGitPromptInfo() (*GitPromptInfo, error) {
	cmd := exec.Command("git", "status", "--porcelain=v2", "--branch", "--show-stash")
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
				info.RefName = "@" + oid[:7]
			} else {
				info.RefName = "@" + oid
			}
		case strings.HasPrefix(line, "# stash "):
			stash := strings.TrimPrefix(line, "# stash ")
			count, err := strconv.ParseUint(stash, 10, 64)
			if err == nil {
				info.StashCount = count
			}
		case strings.HasPrefix(line, "# branch.ab "):
			aheadBehind := strings.TrimPrefix(line, "# branch.ab ")
			aheadBehindSplit := strings.Split(aheadBehind, " ")
			if aheadBy, err := strconv.ParseUint(aheadBehindSplit[0][1:], 10, 64); err == nil {
				info.AheadRemote = aheadBy
			}
			if behindBy, err := strconv.ParseUint(aheadBehindSplit[1][1:], 10, 64); err == nil {
				info.BehindRemote = behindBy
			}

		case line[0] == '?':
			info.UntrackedFiles++
		case line[0] == 'u':
			info.UnmergedFiles++
		case line[0] == '1' || line[0] == '2' || line[0] == 'u':
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
				prompt.WriteString(" " + ansiWhite)
				prompt.WriteString("!" + strconv.FormatUint(jjInfo.ModifiedFiles, 10))
			}
			if jjInfo.UntrackedFiles > 0 {
				prompt.WriteRune(' ')
				if jjInfo.ModifiedFiles > 0 {
					prompt.WriteString(ansiPink)
				}
				prompt.WriteString("?" + strconv.FormatUint(jjInfo.UntrackedFiles, 10))
			}
		} else if gitInfo, err := getGitPromptInfo(); err == nil {
			prompt.WriteString(" " + ansiPink)
			prompt.WriteString(shellEscape(gitInfo.RefName))
			if gitInfo.ModifiedFiles > 0 {
				prompt.WriteString(" " + ansiWhite)
				prompt.WriteString("!" + strconv.FormatUint(gitInfo.ModifiedFiles, 10))
			}
			if gitInfo.UntrackedFiles > 0 {
				prompt.WriteRune(' ')
				if gitInfo.ModifiedFiles > 0 {
					prompt.WriteString(ansiPink)
				}
				prompt.WriteString("?" + strconv.FormatUint(gitInfo.UntrackedFiles, 10))
			}
			if gitInfo.UnmergedFiles > 0 {
				prompt.WriteString(" " + ansiBlue)
				prompt.WriteString("~" + strconv.FormatUint(gitInfo.UnmergedFiles, 10))
			}
			if gitInfo.StagedFiles > 0 {
				prompt.WriteRune(' ')
				if gitInfo.ModifiedFiles > 0 {
					prompt.WriteString(ansiPink)
				}
				prompt.WriteString("+" + strconv.FormatUint(gitInfo.StagedFiles, 10))
			}
			if gitInfo.StashCount > 0 {
				prompt.WriteRune(' ')
				if gitInfo.ModifiedFiles > 0 || gitInfo.UnmergedFiles > 0 {
					prompt.WriteString(ansiPink)
				}
				prompt.WriteString("*" + strconv.FormatUint(gitInfo.StashCount, 10))
			}
			if gitInfo.AheadRemote > 0 {
				prompt.WriteString(" " + ansiWhite)
				prompt.WriteString("a" + strconv.FormatUint(gitInfo.AheadRemote, 10))
			}
			if gitInfo.BehindRemote > 0 {
				prompt.WriteRune(' ')
				if gitInfo.ModifiedFiles > 0 || gitInfo.UnmergedFiles > 0 || gitInfo.AheadRemote > 0 {
					prompt.WriteString(ansiPink)
				}
				prompt.WriteString("b" + strconv.FormatUint(gitInfo.BehindRemote, 10))
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
