package main

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/slack-go/slack"
)

type config struct {
	message              string
	channelName          string
	slackTokenEnvVarName string
	conditions           conditionsConfig
}

type conditionsConfig struct {
	exitCodes []int
	failed    bool
	branches  []string
}

type mapping struct {
	AuthorGitHubHandle string
	// TODO
	// AuthorSlackID      string
	BuildkiteStepLabel string
}

func readPluginEnv(key string) string {
	return os.Getenv(fmt.Sprintf("BUILDKITE_PLUGIN_BUILDKITE_SLACK_PLUGIN_GIT_%s", key))
}

func readConfig() *config {
	cfg := config{}
	cfg.message = readPluginEnv("MESSAGE")
	cfg.channelName = readPluginEnv("CHANNEL_NAME")
	cfg.slackTokenEnvVarName = readPluginEnv("SLACK_TOKEN_ENV_VAR_NAME")

	if cfg.slackTokenEnvVarName == "" {
		cfg.slackTokenEnvVarName = "SLACK_TOKEN"
	}

	branches := readPluginEnv("CONDITIONS_BRANCHES")
	if branches != "" {
		cfg.conditions.branches = strings.Split(branches, ",")
	}

	exitCodes := readPluginEnv("CONDITIONS_EXIT_CODES")
	if exitCodes != "" {
		for _, exitCode := range strings.Split(exitCodes, ",") {
			i, err := strconv.Atoi(exitCode)
			if err != nil {
				log.Fatal(err)
			}
			cfg.conditions.exitCodes = append(cfg.conditions.exitCodes, i)
		}
	}

	cfg.conditions.failed = readPluginEnv("CONDITIONS_FAILED") == "true"

	return &cfg
}

func evaluateConditions(buildkiteExitStatus string, buildkiteBranch string, cfg *config) bool {
	if len(cfg.conditions.branches) > 0 {
		found := false
		for _, branch := range cfg.conditions.branches {
			if branch == buildkiteBranch {
				found = true
				break
			}
		}
		if !found {
			log.Println("no branch conditions matching")
			return false
		}
	}

	if len(cfg.conditions.exitCodes) > 0 {
		buildkiteExitCode, err := strconv.Atoi(buildkiteExitStatus)
		if err != nil {
			log.Fatal(err)
		}
		found := false
		for _, exitCode := range cfg.conditions.exitCodes {
			if exitCode == buildkiteExitCode {
				found = true
				break
			}
		}
		if !found {
			log.Println("no exit code conditions matching")
			return false
		}
	}

	if cfg.conditions.failed && buildkiteExitStatus == "0" {
		return false
	}

	return true
}

func parseMentions(message string) []string {
	re := regexp.MustCompile(`<@((?:[\w- ])+)>`)
	matches := re.FindAllStringSubmatch(message, -1)
	var mentions []string
	for _, m := range matches {
		if len(m) > 1 {
			mentions = append(mentions, m[1:]...)
		}
	}
	return mentions
}

func findMentionsMappings(api *slack.Client, message string) (map[string]string, error) {
	mentions := parseMentions(message)

	m := map[string]string{}
	users, err := api.GetUsers()
	if err != nil {
		return nil, err
	}
	for _, u := range users {
		for _, mention := range mentions {
			if strings.ToLower(u.Profile.DisplayName) == strings.ToLower(mention) {
				m[mention] = fmt.Sprintf("<@%s>", u.ID)
			}
		}
	}
	if len(m) == len(mentions) {
		// if we found everyone, skip searching in groups.
		return m, nil
	}

	groups, err := api.GetUserGroups()
	if err != nil {
		return nil, err
	}
	for _, g := range groups {
		for _, mention := range mentions {
			if strings.ToLower(g.Name) == strings.ToLower(mention) || strings.ToLower(g.Name) == strings.ToLower(strings.ReplaceAll(mention, "-", " ")) {
				m[mention] = fmt.Sprintf("<!subteam^%s>", g.ID)
			}
		}
	}

	if len(m) != len(mentions) {
		return nil, fmt.Errorf("could not find all slack users and groups: %v", m)
	}

	return m, nil
}

func interpolateMentions(api *slack.Client, message string) (string, error) {
	m, err := findMentionsMappings(api, message)
	if err != nil {
		return "", err
	}
	for k, v := range m {
		message = strings.ReplaceAll(message, fmt.Sprintf("<@%s>", k), v)
	}
	return message, nil
}

func main() {
	cfg := readConfig()

	slackToken := os.Getenv(cfg.slackTokenEnvVarName)
	if slackToken == "" {
		log.Fatal("Blank Slack token, aborting")
	}

	buildkiteExitStatus := os.Getenv("BUILDKITE_COMMAND_EXIT_STATUS")
	buildkiteBranch := os.Getenv("BUILDKITE_BRANCH")
	wantMessage := evaluateConditions(buildkiteExitStatus, buildkiteBranch, cfg)
	if !wantMessage {
		fmt.Println("--- :slack: Custom Slack Plugin: no conditions matched, skipping,")
		log.Println("no conditions matching, exiting.")
		os.Exit(0)
	}
	fmt.Println("--- :slack: Custom Slack Plugin: sending out notification.")

	api := slack.New(slackToken)
	var next string
	var targetChannelID string
	// List all channels so we can find the id of the one we're looking for.
LOOP:
	for {
		var channels []slack.Channel
		var err error
		channels, next, err = api.GetConversations(&slack.GetConversationsParameters{
			Cursor:          next,
			ExcludeArchived: true,
			Types:           []string{"public_channel"},
			Limit:           200, // recommended value
		})

		if err != nil {
			log.Fatal(err)
		}

		// Grab the channel ID
		for _, channel := range channels {
			if strings.ToLower(channel.Name) == strings.ToLower(cfg.channelName) {
				targetChannelID = channel.ID
				break LOOP
			}
		}

		if next == "" {
			break
		}
	}

	if targetChannelID == "" {
		log.Fatalf("aborting, could not find channel named %q", cfg.channelName)
	}

	message, err := interpolateMentions(api, "hello <@jh>, <@dev-experience-support>, how is it going?")
	if err != nil {
		log.Fatal(err)
	}

	_, _, err = api.PostMessage(
		targetChannelID,
		slack.MsgOptionText(cfg.message, false),
		slack.MsgOptionBlocks(slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn", message, false, false),
			nil,
			nil,
		)),
	)

	if err != nil {
		log.Fatal(err)
	}
}
