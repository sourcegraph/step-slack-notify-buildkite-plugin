# Step Slack Notify Buildkite Plugin

A small [Buildkite](https://buildkite.com/) plugin to emit Slack notifications when a job failed. 

## Installation

- Requires Go > 1.17.X
- Requires a valid Slack token with the following permissions:
  - `chat:write` (posting messages) 
  - `channels:read` (listing channels) 
  - `usergroups:read` (listing user groups) 
  - `users:read` (listing users) 

## Usage

```yml
steps:
  - label: 'some job'
    command: "ls do_not_exist"
    plugins:
      - https://github.com/sourcegraph/step-slack-notify-buildkite-plugin.git#main:
          message: "something mentioning <@A-User-Group> and <@John Doe>"
          channel_name: "your-channel"
          slack_token_env_var_name: "CI_SLACK_TOKEN" # env var name where is stored the Slack token
          conditions:
            failed: true
```
