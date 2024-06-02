package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
	"strings"

	"github.com/joho/godotenv"
	"github.com/kelseyhightower/envconfig"
	"github.com/mmcdole/gofeed"
)

type Config struct {
	MisskeyHost string   `envconfig:"MISSKEY_HOST" required:"true"`
	AuthToken   string   `envconfig:"AUTH_TOKEN" required:"true"`
	RSSURL      []string `envconfig:"RSS_URL" required:"true"`
}

type MisskeyNote struct {
	Text string `json:"text"`
}

type Cache struct {
	mu         sync.RWMutex
	latestItem time.Time
}

func (c *Cache) getLatestItem() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.latestItem
}

func (c *Cache) saveLatestItem(published time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.latestItem = published
}

func processRSS(config Config, cache *Cache) error {
	for _, rssURL := range config.RSSURL {
		fp := gofeed.NewParser()
		feed, err := fp.ParseURL(rssURL)
		if err != nil {
			log.Println("RSSのパースが上手くできませんでした。: / Failed to parse RSS:", err)
			return err
		}

		latestItem := cache.getLatestItem()

		// Clean linebreak tags (is there a better way to do this through gofeed?)
		cleanedContent := strings.ReplaceAll(feed.Items[0].Content, "<br />", "")
		feed.Items[0].Content = cleanedContent

		log.Println("Feed Title:", feed.Title)
		log.Println("Feed Description:", feed.Description)
		log.Println("Feed Link:", feed.Link)

		if len(feed.Items) > 0 && feed.Items[0].PublishedParsed != nil {
			newestItem := *feed.Items[0].PublishedParsed

			if newestItem.After(latestItem) {

				err := postToMisskey(config, feed.Items[0])
				if err != nil {
					log.Println("Misskeyの投稿をしくじりました...: / Failed to post to Misskey:", err)
					return err
				} else {
					log.Println("Misskeyに投稿しました。: / Posted to Misskey:", feed.Items[0].Title)

					cache.saveLatestItem(newestItem)
				}
			}
		}
	}

	return nil
}

func getLatestItem(cache *Cache) time.Time {

	return cache.getLatestItem()
}

func saveLatestItem(cache *Cache, published time.Time) {

	cache.saveLatestItem(published)
}

func postToMisskey(config Config, item *gofeed.Item) error {

	note := map[string]interface{}{
		"i":          config.AuthToken,
		"text":       fmt.Sprintf("%s", item.Content),
		"visibility": "home",
	}

	payload, err := json.Marshal(note)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("https://%s/api/notes/create", config.MisskeyHost)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(payload))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", config.AuthToken)

	client := http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("MisskeyAPIと以下の理由で接続を確立できません: %d / Failed to connect to Misskey API for the following reason: %d", resp.StatusCode)
	}

	return nil
}

func main() {
	fmt.Println("処理を開始しますっ！ / Starting process!")

	err := godotenv.Load()
	if err != nil {
		log.Fatal(".envファイルの読み込みに失敗しました。入力した内容を確認してください。: / Failed to load .env file. Please check the input content:", err)
	}

	var config Config
	err = envconfig.Process("", &config)
	if err != nil {
		log.Fatal("環境変数の読み込みをしくじりました...: / Failed to load environment variables:", err)
	}

	cache := &Cache{}

	//RSSを取得する間隔です。今回は結構頻繁に更新される事例を想定して短めに持たせているけど、NHKとかだと５分スパンで十分です。/ This is the interval for retrieving RSS. This time, it is set short assuming a case that is updated quite frequently, but for something like NHK, a 5-minute span is sufficient.
	//分数で指定する場合はtime.Minuteに書き換えてください。 / If specifying in minutes, change to time.Minute.
	interval := 30 * time.Second
	ticker := time.NewTicker(interval)

	for {
		select {
		case <-ticker.C:
			log.Println("最新のRSS情報を取得しています / Retrieving the latest RSS information")
			errProcessRSS := processRSS(config, cache)
			if errProcessRSS != nil {
				log.Println("RSSの取得に失敗しました...: / Failed to retrieve RSS:", errProcessRSS)
			}
			log.Println("最新のRSS情報を取得しました / Retrieved the latest RSS information")
		}
	}
}