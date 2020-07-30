package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"gopkg.in/yaml.v2"
	"io"
	"io/ioutil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/dghubble/go-twitter/twitter"
	"github.com/dghubble/oauth1"
	"github.com/makeworld-the-better-one/go-gemini"
)

type Config struct {
	Addr struct {
		Host string `yaml:"host"`
		Port int    `yaml:"port"`
	} `yaml:"addr"`
	Cert struct {
		CertFile string `yaml:"certFile"`
		KeyFile  string `yaml:"keyFile"`
	} `yaml:"cert"`
	Twitter struct {
		ConsumerKey    string `yaml:"consumerKey"`
		ConsumerSecret string `yaml:"consumerSecret"`
		AccessToken    string `yaml:"accessToken"`
		AccessSecret   string `yaml:"accessSecret"`
		UserID         int64  `yaml:"userID"`
		ScreenName     string `yaml:"screenName"`
	} `yaml:"twitter"`
	UI struct {
		AsciiLogoFile string `yaml:"asciiLogoFile"`
		Delimiter     string `yaml:"delimiter"`
	} `yaml:"ui"`
}

func (c *Config) Parse(path string) {
	f, err := os.Open(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	decoder := yaml.NewDecoder(f)
	err = decoder.Decode(&c)
	if err != nil {
		panic(err)
	}
}

type TweetCache struct {
	Config
	Tweets      []twitter.Tweet
	LastRefresh time.Time
}

func (tc *TweetCache) Refresher() {
	for {
		if time.Since(tc.LastRefresh) < time.Minute*15 {
			continue
		}

		tweets, err := tc.getTweets()

		if err != nil {
			time.Sleep(time.Minute * 5)
		} else if len(tweets) < len(tc.Tweets) {
			continue
		} else {
			tc.Tweets = tweets
			tc.LastRefresh = time.Now()
		}
	}
}

func (tc *TweetCache) GetOnPosition(pos int) (string, error) {
	if len(tc.Tweets) == 0 || len(tc.Tweets)-1 < pos {
		return "", errors.New("twit not available")
	}
	tweet := tc.Tweets[pos]
	return tweet.Text + "\n\n" + tweet.User.Name, nil
}

func (tc *TweetCache) getTweets() ([]twitter.Tweet, error) {
	config := oauth1.NewConfig(tc.Config.Twitter.ConsumerKey, tc.Config.Twitter.ConsumerSecret)
	token := oauth1.NewToken(tc.Config.Twitter.AccessToken, tc.Config.Twitter.AccessSecret)
	httpClient := config.Client(oauth1.NoContext, token)
	client := twitter.NewClient(httpClient)

	t := true
	tweets, _, err := client.Timelines.UserTimeline(&twitter.UserTimelineParams{
		UserID:         tc.Config.Twitter.UserID,
		ScreenName:     tc.Config.Twitter.ScreenName,
		Count:          100,
		ExcludeReplies: &t,
	})
	if err != nil {
		return nil, err
	}
	return tweets, nil
}

type RequestHandler struct {
	TweetCache *TweetCache
	Config
}

func (rh *RequestHandler) getFooter() string {
	return `

=> https://github.com/vegasq/gemini-twitter-mirror Fork me on GitHub
`
}

func (rh *RequestHandler) getHeader() string {
	var logo string
	fl, err := os.Open(rh.Config.UI.AsciiLogoFile)
	if err == os.ErrNotExist {
		logo = ""
	} else {
		b := strings.Builder{}
		io.Copy(&b, fl)
		logo = b.String()
	}
	return fmt.Sprintf(`%s

=> / Last tweet
=> /timeline Timeline
=> /select_tweet Tweet selector

`, logo)
}

func (rh *RequestHandler) formatTimeline() string {
	var timeline string
	for i := 0; i < 10; i += 1 {
		tw, err := rh.TweetCache.GetOnPosition(i)
		if err != nil {
			continue
		}

		timeline += fmt.Sprintf("\n\n%s\n\n%s", tw, rh.Config.UI.Delimiter)
	}
	return timeline
}

func (rh *RequestHandler) formatTweet(pos int) string {
	tw, err := rh.TweetCache.GetOnPosition(pos)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("\n\n%s", tw)
}

func (rh *RequestHandler) wrapBody(body string) string {
	return fmt.Sprintf("%s%s%s", rh.getHeader(), body, rh.getFooter())
}

func (rh *RequestHandler) showTweet(offset int) *gemini.Response {
	body := ioutil.NopCloser(bytes.NewBufferString(rh.wrapBody(rh.formatTweet(offset))))
	return &gemini.Response{20, "text/gemini", body, nil}
}

func (rh *RequestHandler) showTimeline() *gemini.Response {
	body := ioutil.NopCloser(bytes.NewBufferString(rh.wrapBody(rh.formatTimeline())))
	return &gemini.Response{20, "text/gemini", body, nil}
}

func getFirstKeyFromURL(u url.URL) string {
	params := u.Query()
	for k := range params {
		return k
	}
	return ""
}

func (rh *RequestHandler) Handle(r gemini.Request) *gemini.Response {
	params := r.URL.Query()
	if r.URL.Path == "/" {
		return rh.showTweet(0)
	} else if r.URL.Path == "/timeline" {
		return rh.showTimeline()
	} else if r.URL.Path == "/select_tweet" && len(params) == 0 {
		return &gemini.Response{10, "Get tweet offset. f.e. 5", nil, nil}
	} else if r.URL.Path == "/select_tweet" {
		offset, err := strconv.Atoi(getFirstKeyFromURL(*r.URL))
		if err != nil {
			return &gemini.Response{42, "Failed to parse input. Please use numbers.", nil, nil}
		}
		return rh.showTweet(offset)
	}
	return &gemini.Response{51, "Unknown location", nil, nil}
}

func main() {
	var path string
	flag.StringVar(&path, "config", "config.yml", "Location of config file")
	flag.Parse()

	c := Config{}
	c.Parse(path)

	tc := TweetCache{Config: c}
	go tc.Refresher()

	err := gemini.ListenAndServe(
		fmt.Sprintf("%s:%d", c.Addr.Host, c.Addr.Port),
		c.Cert.CertFile,
		c.Cert.KeyFile,
		&RequestHandler{&tc, c},
	)
	if err != nil {
		fmt.Println(err)
	}
}
