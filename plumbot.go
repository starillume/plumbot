package main

import (
	"encoding/json"
	"fmt"
	"github.com/bwmarrin/discordgo"
	"github.com/joho/godotenv"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"
)

type CommitRequest struct {
	SHA     string `json:"sha"`
	Commit  `json:"commit"`
	HTMLURL string `json:"html_url"`
}

type Commit struct {
	Message string `json:"message"`
	Author  Author `json:"author"`
}

type Author struct {
	Name string    `json:"name"`
	Date time.Time `json:"date"`
}

func getFeatCommits(repo, token string) ([]CommitRequest, error) {
	url := "https://api.github.com/repos/" + repo + "/commits"
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "plumbot")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github api error: %s - %s", resp.Status, string(body))
	}

	var commits []CommitRequest
	if err := json.NewDecoder(resp.Body).Decode(&commits); err != nil {
		return nil, err
	}

	re := regexp.MustCompile(`(?i)^feat(\(.*\))?:`)
	var feats []CommitRequest
	for _, c := range commits {
		if re.MatchString(c.Commit.Message) {
			feats = append(feats, c)
		}
	}

	return feats, nil
}

func formatCommitMessage(c CommitRequest) string {
	re := regexp.MustCompile(`(?is)^feat(?:\(([^)]+)\))?:\s*(.*)$`)
	matches := re.FindStringSubmatch(c.Commit.Message)

	var scope, msg string
	if len(matches) > 2 {
		scope = matches[1]
		msg = matches[2]
	} else {
		msg = c.Commit.Message
	}

	msg = strings.SplitN(msg, "\n", 2)[0]

	coAuthorRe := regexp.MustCompile(`(?mi)^co-authored-by:\s*([^<\n]+)`)
	coAuthors := coAuthorRe.FindAllStringSubmatch(c.Commit.Message, -1)

	var names []string
	for _, author := range coAuthors {
		name := strings.TrimSpace(author[1])
		if name != "" {
			names = append(names, name)
		}
	}

	if scope != "" {
		msg = fmt.Sprintf("%s: %s", scope, msg)
	}

	by := c.Commit.Author.Name
	for i, name := range names {
		if i == len(names) - 1 {
			by += " and " + name
		} else {
			by += ", " + name
		}
	}

	return fmt.Sprintf("[%s](%s)\nby %s, at %s\n", strings.TrimSpace(msg), c.HTMLURL, by, c.Commit.Author.Date.Format("02/01/2006, 15:03 PM"))
}
func loadCache() string {
	data, err := os.ReadFile(".cache")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func saveCache(sha string) {
	os.WriteFile(".cache", []byte(sha), 0644)
}

func sendNewCommits(dg *discordgo.Session, channelID, repo, token string) error {
	feats, err := getFeatCommits(repo, token)
	if err != nil {
		return err
	}
	if len(feats) == 0 {
		return nil
	}

	lastSent := loadCache()
	newCommits := []CommitRequest{}

	for _, c := range feats {
		if c.SHA == lastSent {
			break
		}
		newCommits = append(newCommits, c)
	}

	if len(newCommits) == 0 {
		return nil
	}

	msg := "there are new features in plum priestess!\n\n"
	for i := len(newCommits) - 1; i >= 0; i-- {
		msg += formatCommitMessage(newCommits[i]) + "\n"
	}

	_, err = dg.ChannelMessageSend(channelID, msg)
	if err != nil {
		return err
	}

	saveCache(feats[0].SHA)
	return nil
}

func main() {
	if err := godotenv.Load(); err != nil {
		fmt.Println("warning: no .env file found")
	}

	token := os.Getenv("DISCORD_TOKEN")
	channelID := os.Getenv("DISCORD_CHANNEL_ID")
	repo := os.Getenv("GITHUB_REPO")
	githubToken := os.Getenv("GITHUB_TOKEN")

	if token == "" || channelID == "" || repo == "" || githubToken == "" {
		fmt.Println("missing required env vars: DISCORD_TOKEN, DISCORD_CHANNEL_ID, GITHUB_REPO, GITHUB_TOKEN")
		os.Exit(1)
	}

	dg, err := discordgo.New("Bot " + token)
	if err != nil {
		fmt.Println("error creating discord session: ", err)
		os.Exit(1)
	}

	if err := sendNewCommits(dg, channelID, repo, githubToken); err != nil {
		fmt.Println("error sending message: ", err)
	}

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		<-ticker.C
		if err := sendNewCommits(dg, channelID, repo, githubToken); err != nil {
			fmt.Println("error sending message: ", err)
		}
	}
}
