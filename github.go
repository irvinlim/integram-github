package github

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	gh "github.com/google/go-github/github"
	"github.com/requilence/integram"
	"golang.org/x/oauth2"
)

var m = integram.HTMLRichText{}

type Config struct {
	integram.OAuthProvider
	integram.BotConfig
}

func (c Config) Service() *integram.Service {
	return &integram.Service{
		Name:                "github",
		NameToPrint:         "GitHub",
		WebhookHandler:      webhookHandler,
		TGNewMessageHandler: messageHandler,
		JobsPool:            1,
		Jobs: []integram.Job{
			{cacheNickMap, 10, integram.JobRetryFibonacci},
		},
		DefaultOAuth2: &integram.DefaultOAuth2{
			Config: oauth2.Config{
				ClientID:     c.ID,
				ClientSecret: c.Secret,
				Endpoint: oauth2.Endpoint{
					AuthURL:  c.OAuthProvider.BaseURL.String() + path.Join(c.OAuthProvider.BaseURL.GetPath(), "/oauth/authorize"),
					TokenURL: c.OAuthProvider.BaseURL.String() + path.Join(c.OAuthProvider.BaseURL.GetPath(), "/oauth/token"),
				},
			},
		},
		OAuthSuccessful: oAuthSuccessful,
	}
}
func client(c *integram.Context) *gh.Client {
	client := gh.NewClient(c.User.OAuthHTTPClient())
	parsedURL, _ := url.Parse(c.ServiceBaseURL.String())
	client.BaseURL = parsedURL
	return client
}

func me(c *integram.Context) (*gh.User, error) {
	user := &gh.User{}

	c.User.Cache("me", user)
	if user.GetID() > 0 {
		return user, nil
	}

	user, _, err := client(c).Users.Get(context.Background(), "")
	if err != nil {
		return nil, err
	}

	if err := c.User.SetCache("me", user, time.Hour*24*30); err != nil {
		c.Log().WithError(err).Error("could not set cache for user")
	}

	return user, nil
}

func cacheNickMap(c *integram.Context) error {
	me, err := me(c)
	if err != nil {
		return err
	}

	if err := c.SetServiceCache("nick_map_"+me.GetName(), c.User.UserName, time.Hour*24*365); err != nil {
		c.Log().WithError(err).Error("could not set cache for nick map")
	}

	if err := c.SetServiceCache("nick_map_"+me.GetEmail(), c.User.UserName, time.Hour*24*365); err != nil {
		c.Log().WithError(err).Error("could not set cache for nick map")
	}

	return nil
}

func oAuthSuccessful(c *integram.Context) error {
	if _, err := c.Service().SheduleJob(cacheNickMap, 0, time.Now().Add(time.Second*5), c); err != nil {
		c.Log().WithError(err).Error("could not schedule job for cacheNickMap")
	}

	return c.NewMessage().SetText("Great! Now you can reply issues, commits, merge requests and snippets").Send()
}

func mention(c *integram.Context, name string, email string) string {
	userName := ""
	c.ServiceCache("nick_map_"+name, &userName)
	if userName == "" && email != "" {
		c.ServiceCache("nick_map_"+email, &userName)
	}
	if userName == "" {
		return m.Bold(name)
	}
	return "@" + userName
}

func webhookHandler(c *integram.Context, request *integram.WebhookContext) error {
	// Prepare new outgoing message
	msg := c.NewMessage()

	// Get event type from request header
	eventType := request.Header("X-GitHub-Event")
	switch eventType {
	case "push":
		var p gh.PushEvent

		// Parse payload
		if err := request.JSON(&p); err != nil {
			return fmt.Errorf("could not parse payload: %s", err)
		}

		// Get repository URL
		var repoURL string
		if p.Repo.GetURL() != "" {
			repoURL = p.Repo.GetURL()
		} else if p.Repo.GetHomepage() != "" {
			repoURL = p.Repo.GetHomepage()
		}
		if repoURL == "" {
			c.Log().WithField("payload", p).Error("github webhook empty url")
		} else {
			c.SetServiceBaseURL(repoURL)
		}

		// Get branch name
		refParts := strings.Split(p.GetRef(), "/")
		branch := refParts[len(refParts)-1]

		// TODO: Handle payload with empty commits
		if len(p.Commits) <= 0 {
			c.Log().WithField("payload", p).Error("empty commits")
			return fmt.Errorf("unhandled case for empty commits in PushEvent")
		}

		var added, removed, modified int

		// Prepare pusher
		pusher := mention(c, p.Pusher.GetName(), p.Pusher.GetEmail())
		if p.Pusher.GetURL() != "" {
			pusher = m.URL(pusher, p.Pusher.GetURL())
		}

		// Prepare WebPreview
		var wpBody string

		anyOherPersonCommits := false
		for _, commit := range p.Commits {
			if commit.Author.GetEmail() != p.Pusher.GetEmail() && commit.Author.GetName() != p.Pusher.GetName() {
				anyOherPersonCommits = true
			}
		}

		for _, commit := range p.Commits {
			commitMsg := strings.TrimSuffix(commit.GetMessage(), "\n")
			if anyOherPersonCommits {
				wpBody += mention(c, commit.Author.GetName(), commit.Author.GetEmail()) + ": "
			}
			wpBody += m.URL(commitMsg, commit.GetURL()) + "\n"
			added += len(commit.Added)
			removed += len(commit.Removed)
			modified += len(commit.Modified)
		}

		var filesChanged string
		if modified > 0 {
			filesChanged += strconv.Itoa(modified) + " files modified"
		}
		if added > 0 {
			if filesChanged == "" {
				filesChanged += strconv.Itoa(added) + " files added"
			} else {
				filesChanged += " " + strconv.Itoa(added) + " added"
			}
		}
		if removed > 0 {
			if filesChanged == "" {
				filesChanged += strconv.Itoa(removed) + " files removed"
			} else {
				filesChanged += " " + strconv.Itoa(removed) + " removed"
			}
		}

		wp := c.WebPreview(
			fmt.Sprintf("%d commits", len(p.Commits)),
			"@"+p.GetBefore()[0:10]+" ... @"+p.GetAfter()[0:10],
			filesChanged,
			p.GetCompare(),
			p.Pusher.GetAvatarURL(),
		)

		c.Log().WithField("url", repoURL+"/tree/"+url.QueryEscape(branch)).Info("tree url")

		text := fmt.Sprintf("%s %s to %s\n%s",
			pusher,
			m.URL("pushed", wp),
			m.URL(p.Repo.GetFullName()+"/"+branch, repoURL+"/tree/"+url.QueryEscape(branch)),
			wpBody,
		)

		lastCommit := p.Commits[len(p.Commits)-1]

		if err := c.Chat.SetCache("commit_"+lastCommit.GetID(), text, time.Hour*24*30); err != nil {
			c.Log().WithError(err).Error("could not set cache")
		}

		return msg.AddEventID("commit_" + lastCommit.GetID()).SetText(text).EnableHTML().Send()
	}

	c.Log().WithField("type", eventType).Error("handler for event type not implemented")
	return errors.New("handler not implemented")
}

func messageHandler(c *integram.Context) error {
	command, param := c.Message.GetCommand()

	if c.Message.IsEventBotAddedToGroup() {
		command = "start"
	}
	if param == "silent" {
		command = ""
	}

	switch command {

	case "start":
		text := "Hi here! To setup notifications " + m.Bold("for this chat") +
			" your GitHub repo, open Settings -> Webhooks and add this URL:\n" +
			m.Fixed(c.Chat.ServiceHookURL())

		return c.NewMessage().EnableAntiFlood().EnableHTML().SetText(text).EnableHTML().Send()

	case "cancel", "clean", "reset":
		return c.NewMessage().SetText("Clean").HideKeyboard().Send()

	}

	return nil
}
