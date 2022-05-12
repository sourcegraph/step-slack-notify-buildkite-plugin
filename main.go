package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/slack-go/slack"
)

type config struct {
	Message              string           `json:"message"`
	ChannelName          string           `json:"channel_name"`
	SlackTokenEnvVarName string           `json:"slack_token_env_var_name"`
	Conditions           conditionsConfig `json:"conditions"`
}

type conditionsConfig struct {
	ExitCodes []int    `json:"exit_codes"`
	Failed    bool     `json:"failed"`
	Branches  []string `json:"branches"`
}

func readConfig(buildkitePlugins string) (*config, error) {
	plugins := []map[string]json.RawMessage{}
	err := json.Unmarshal([]byte(buildkitePlugins), &plugins)
	if err != nil {
		return nil, err
	}

	var rawConfig json.RawMessage
LOOP:
	for _, rawPlugin := range plugins {
		for k, v := range rawPlugin {
			if strings.Contains(k, "sourcegraph/step-slack-notify-buildkite-plugin") {
				rawConfig = v
				break LOOP
			}
		}
	}
	if rawConfig == nil {
		return nil, fmt.Errorf("cannot find configuration")
	}

	cfg := config{}
	err = json.Unmarshal(rawConfig, &cfg)
	if err != nil {
		return nil, err
	}

	if cfg.SlackTokenEnvVarName == "" {
		cfg.SlackTokenEnvVarName = "SLACK_TOKEN"
	}

	return &cfg, nil
}

func evaluateConditions(buildkiteExitStatus string, buildkiteBranch string, cfg *config) bool {
	if len(cfg.Conditions.Branches) > 0 {
		found := false
		for _, branch := range cfg.Conditions.Branches {
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

	if len(cfg.Conditions.ExitCodes) > 0 {
		buildkiteExitCode, err := strconv.Atoi(buildkiteExitStatus)
		if err != nil {
			log.Fatal(err)
		}
		found := false
		for _, exitCode := range cfg.Conditions.ExitCodes {
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

	if cfg.Conditions.Failed && buildkiteExitStatus == "0" {
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
	cfg, err := readConfig(os.Getenv("BUILDKITE_PLUGINS"))
	if err != nil {
		log.Fatalf("failed to read config: %q", err)
	}

	slackToken := os.Getenv(cfg.SlackTokenEnvVarName)
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
			if strings.ToLower(channel.Name) == strings.ToLower(cfg.ChannelName) {
				targetChannelID = channel.ID
				break LOOP
			}
		}

		if next == "" {
			break
		}
	}

	if targetChannelID == "" {
		log.Fatalf("aborting, could not find channel named %q", cfg.ChannelName)
	}

	message, err := interpolateMentions(api, cfg.Message)
	if err != nil {
		log.Fatal(err)
	}

	blocks := []slack.Block{
		slack.NewSectionBlock(
			slack.NewTextBlockObject("mrkdwn", message, false, false),
			nil,
			nil,
		),
		slack.NewDividerBlock(),
	}

	if buildkiteExitStatus != "0" {
		blocks = append(blocks, slack.NewContextBlock("context", slack.NewTextBlockObject("mrkdwn",
			fmt.Sprintf(
				"*<%s|:point_right: View logs :point_left:>* <%s|%s/%s: Build %s> :red_circle:",
				fmt.Sprintf("%s#%s", os.Getenv("BUILDKITE_BUILD_URL"), os.Getenv("BUILDKITE_JOB_ID")),
				os.Getenv("BUILDKITE_BUILD_URL"),
				os.Getenv("BUILDKITE_ORGANIZATION_SLUG"),
				os.Getenv("BUILDKITE_PIPELINE_NAME"),
				os.Getenv("BUILDKITE_BUILD_NUMBER"),
			),
			false,
			false),
		))
	} else {
		blocks = append(blocks, slack.NewContextBlock("context", slack.NewTextBlockObject("mrkdwn",
			fmt.Sprintf(
				"<%s|%s/%s: Build %s> :large_green_circle:",
				os.Getenv("BUILDKITE_BUILD_URL"),
				os.Getenv("BUILDKITE_ORGANIZATION_SLUG"),
				os.Getenv("BUILDKITE_PIPELINE_NAME"),
				os.Getenv("BUILDKITE_BUILD_NUMBER"),
			),
			false,
			false),
		))
	}

	_, _, err = api.PostMessage(
		targetChannelID,
		slack.MsgOptionText(cfg.Message, false),
		slack.MsgOptionBlocks(blocks...))

	if err != nil {
		log.Fatal(err)
	}
}
