package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

const (
	ansiBlue  = "\\[\033[34m\\]"
	ansiPink  = "\\[\033[35m\\]"
	ansiWhite = "\\[\033[97m\\]"
	ansiReset = "\\[\033[0m\\]"
)

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

type promptBuilder struct {
	sb    strings.Builder
	color string
}

func (p *promptBuilder) setColor(c string) {
	if p.color != c {
		p.sb.WriteString(c)
		p.color = c
	}
}

func (p *promptBuilder) writeColored(c, s string) {
	p.setColor(c)
	p.sb.WriteString(s)
}

func (p *promptBuilder) writeStat(c, prefix string, n uint64) {
	if n == 0 {
		return
	}
	p.sb.WriteByte(' ')
	p.setColor(c)
	p.sb.WriteString(prefix)
	p.sb.WriteString(strconv.FormatUint(n, 10))
}

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
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	out, err := exec.CommandContext(ctx, "jj", "log", "--no-graph", "-r", "@", "--color=never", "-T", jjTemplate).Output()
	if err != nil {
		return nil, err
	}

	var info JJPromptInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func getGitPromptInfo() (*GitPromptInfo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	out, err := exec.CommandContext(ctx, "git", "--no-optional-locks", "status", "--porcelain=v2", "--branch", "--show-stash").Output()
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
					if aheadBy, err := strconv.ParseUint(aheadStr[1:], 10, 64); err == nil {
						info.AheadRemote = aheadBy
					}
				}
				if len(behindStr) > 1 {
					if behindBy, err := strconv.ParseUint(behindStr[1:], 10, 64); err == nil {
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
			shortPaths[i] = string(runes[:3])
		} else if !strings.HasPrefix(path, ".") && len(runes) > 2 {
			shortPaths[i] = string(runes[:2])
		} else {
			shortPaths[i] = path
		}
	}
	return strings.Join(shortPaths, "/")
}

func main() {
	if len(os.Args) < 2 {
		var prompt promptBuilder
		prompt.sb.WriteRune(' ')
		if envName, present := getPythonVenv(); present {
			prompt.sb.WriteRune('(')
			prompt.sb.WriteString(envName)
			prompt.sb.WriteString(") ")
		}
		prompt.writeColored(ansiBlue, shellEscape(reducePathWidth(getCwd())))
		if jjInfo, err := getJJPromptInfo(); err == nil {
			prompt.sb.WriteRune(' ')
			prompt.writeColored(ansiPink, shellEscape(jjInfo.ChangeIDPrefix))
			if len(jjInfo.Bookmarks) > 0 {
				prompt.writeColored(ansiWhite, " (")
				prompt.sb.WriteString(shellEscape(strings.Join(jjInfo.Bookmarks, " ")))
				prompt.sb.WriteString(")")
			}
			prompt.writeStat(ansiWhite, "!", jjInfo.ModifiedFiles)
			prompt.writeStat(ansiPink, "?", jjInfo.UntrackedFiles)
		} else if gitInfo, err := getGitPromptInfo(); err == nil {
			prompt.sb.WriteRune(' ')
			prompt.writeColored(ansiPink, shellEscape(gitInfo.RefName))
			prompt.writeStat(ansiWhite, "!", gitInfo.ModifiedFiles)
			prompt.writeStat(ansiPink, "?", gitInfo.UntrackedFiles)
			prompt.writeStat(ansiBlue, "~", gitInfo.UnmergedFiles)
			prompt.writeStat(ansiPink, "+", gitInfo.StagedFiles)
			prompt.writeStat(ansiPink, "*", gitInfo.StashCount)
			prompt.writeStat(ansiWhite, "a", gitInfo.AheadRemote)
			prompt.writeStat(ansiPink, "b", gitInfo.BehindRemote)
		}
		prompt.sb.WriteString(" " + ansiBlue + "λ" + ansiReset + " ")
		fmt.Print(prompt.sb.String())
		return
	}
	switch os.Args[1] {
	case "init":
		fmt.Println(initScript)
	default:
		fmt.Println("Unknown command")
	}
}
