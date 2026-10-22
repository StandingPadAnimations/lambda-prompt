package main

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
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
	ChangedFiles   int
	UntrackedFiles int
}

type JJPromptInfo struct {
	ChangeIDPrefix string   `json:"change_id_prefix"`
	Bookmarks      []string `json:"bookmarks"`
	Empty          bool     `json:"empty"`
	ModifiedFiles  int      `json:"modified_files"`
	UntrackedFiles int      `json:"untracked_files"`
}

func getJJPromptInfo() (*JJPromptInfo, error) {
	// Outsource as much work to jj
	// because we can do it in a single
	// command.
	cmd, err := exec.Command("jj", "log", "--no-graph", "-r", "@", "--color=never", "-T", "json({'change_id_prefix' => change_id.shortest().prefix(), 'bookmarks' => bookmarks.map(|b| b.name()), 'empty' => empty, 'modified_files' => self.diff().files().filter(|f| f.status() == 'modified' || f.status() == 'removed' || f.status() == 'renamed').len(), 'untracked_files' => self.diff().files().filter(|f| f.status() == 'added' || f.status() == 'copied').len()})").Output()
	if err != nil {
		return nil, err
	}
	changeId := strings.TrimSpace(string(cmd))

	var info JJPromptInfo
	if err := json.Unmarshal([]byte(changeId), &info); err != nil {
		return nil, err
	}

	return &info, nil
}

func getGitPromptInfo() (*GitPromptInfo, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}

	repo, err := git.PlainOpenWithOptions(cwd, &git.PlainOpenOptions{
		DetectDotGit: true,
	})
	if err != nil {
		return nil, err
	}

	headRef, err := repo.Head()
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return &GitPromptInfo{RefName: "unborn", ChangedFiles: 0, UntrackedFiles: 0}, nil
		}
		return nil, err
	}

	var name string
	if headRef.Name().IsBranch() {
		name = headRef.Name().Short()
	} else {
		hash := headRef.Hash().String()
		if len(hash) >= 7 {
			name = ":" + hash[:7]
		} else {
			name = ":" + hash
		}
	}

	worktree, err := repo.Worktree()
	changedFiles := 0
	untrackedFiles := 0
	if err == nil {
		status, err := worktree.Status()
		if err == nil {
			for _, fileStatus := range status {
				switch fileStatus.Worktree {
				case git.Untracked:
					untrackedFiles++
				case git.Deleted, git.Renamed, git.Modified:
					changedFiles++
				}
			}
		}
	}

	return &GitPromptInfo{
		RefName:        name,
		ChangedFiles:   changedFiles,
		UntrackedFiles: untrackedFiles,
	}, nil
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
		prompt := fmt.Sprintf("%s%s", ansiBlue, reducePathWidth(getCwd()))
		refIndicator := ""
		changedIndicator := ""
		untrackedIndicator := ""
		if jjInfo, err := getJJPromptInfo(); err == nil {
			refIndicator = jjInfo.ChangeIDPrefix
			if !jjInfo.Empty {
				if jjInfo.ModifiedFiles > 0 {
					changedIndicator = fmt.Sprintf("!%d", jjInfo.ModifiedFiles)
				}
				if jjInfo.UntrackedFiles > 0 {
					untrackedIndicator = fmt.Sprintf("+%d", jjInfo.UntrackedFiles)
				}
			}
			if len(jjInfo.Bookmarks) > 0 {
				refIndicator = fmt.Sprintf("%s %s(%s)%s", refIndicator, ansiWhite, strings.Join(jjInfo.Bookmarks, " "), ansiPink)
			}
		} else if gitInfo, err := getGitPromptInfo(); err == nil {
			refIndicator = gitInfo.RefName
			if gitInfo.ChangedFiles > 0 {
				changedIndicator = fmt.Sprintf("!%d", gitInfo.ChangedFiles)
			}
			if gitInfo.UntrackedFiles > 0 {
				untrackedIndicator = fmt.Sprintf("+%d", gitInfo.UntrackedFiles)
			}
		}
		if refIndicator != "" {
			prompt = strings.TrimSpace(fmt.Sprintf("%s %s⑂ %s %s %s", prompt, ansiPink, refIndicator, changedIndicator, untrackedIndicator))
		}
		prompt = fmt.Sprintf(" %s %sλ%s ", prompt, ansiBlue, ansiReset)
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
