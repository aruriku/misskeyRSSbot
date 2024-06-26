package main

import (
	"bytes"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

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
	latestItem string
}

type Image struct {
	URL string
	ID  string
}

func (c *Cache) getLatestItem() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.latestItem
}

func (c *Cache) saveLatestItem(GUID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.latestItem = GUID
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
		includesImage := false

		// Clean linebreak tags (is there a better way to do this through gofeed?)
		cleanedContent := strings.ReplaceAll(feed.Items[0].Description, "<br>", "\n")

		// Fix &amp which breaks links
		cleanedContent = strings.ReplaceAll(cleanedContent, "&amp;", "&")

		// Remove replace quote with something more readable
		// TODO: interface with the misskey api to search for a post and renote with it's ID, may need to configure search.
		// or... do an expanded post with the quote renote?
		cleanedContent = strings.ReplaceAll(cleanedContent, "<div class=\"rsshub-quote\">", "\n**🔁 Quote:**")

		var images []Image

		// Extract url from image source from Content
		re := regexp.MustCompile(`<img[^>]+src="([^"]+)"`)
		matches := re.FindAllStringSubmatch(feed.Items[0].Description, -1)
		for _, match := range matches {
			if len(match) > 1 {
				imageURL := match[1]
				imageURL = strings.ReplaceAll(imageURL, "&amp;", "&") // clean out garbage in the url (redundant now, but left in just incase)
				includesImage = true
				images = append(images, Image{URL: imageURL})
			}
		}

		// parse videos as well, similar enough that code can just be copy and pasted with minimal edits
		re = regexp.MustCompile(`<video[^>]+src="([^"]+)"`)
		matches = re.FindAllStringSubmatch(feed.Items[0].Description, -1)
		for _, match := range matches {
			if len(match) > 1 {
				imageURL := match[1]
				imageURL = strings.ReplaceAll(imageURL, "&amp;", "&") // clean out garbage in the url (redundant now, but left in just incase)
				includesImage = true
				images = append(images, Image{URL: imageURL})
			}
		}

		// now clean the URL
		re = regexp.MustCompile(`(<img[^>]+\>)|(<p>)|(</p>)`)
		cleanedContent = re.ReplaceAllString(cleanedContent, "")

		// Regex pattern to match <a> tags and capture the href content
		re = regexp.MustCompile(`<a[^>]*href="([^"]+)"[^>]*>([^<]*)</a>`)

		// Replace the matched <a> tags with the content of the href attribute
		cleanedContent = re.ReplaceAllStringFunc(cleanedContent, func(m string) string {
			matches := re.FindStringSubmatch(m)
			if len(matches) > 1 {
				return matches[1]
			}
			return m
		})

		// clean any remaining html tags
		re = regexp.MustCompile(`<[^>]*>`)
		cleanedContent = re.ReplaceAllString(cleanedContent, "")

		//assign the cleaned post
		feed.Items[0].Description = cleanedContent

		log.Println("Feed Title:", feed.Title)
		log.Println("Feed Description:", feed.Description)
		log.Println("Feed Link:", feed.Link)

		if len(feed.Items) > 0 && feed.Items[0].GUID != "" {
			newestItem := feed.Items[0].GUID

			if newestItem != latestItem {
				if includesImage {
					for i := range images {
						log.Println("image url:", images[i].URL)

						err := UploadImage(config, images[i].URL)
						if err != nil {
							log.Println("Misskeyへの画像アップロードに失敗しました... / Failed to upload image to Misskey:", err)
						} else {
							log.Println("Uploaded image")
							log.Println("searching image...")
							images[i].ID, err = SearchForImage(config, images[i].URL)
							log.Println("Image ID:", images[i].ID)
							// if err, return immediately. if it returns 0, wait a couple seconds and retry, then fail if it doesn't after that request as well
							if err != nil {
								log.Println("画像の検索に失敗しました / Failed to search for image:", err)
								return err
							}
							if images[i].ID == "0" { // realistically an ID should never be 0 (though I've never actually confirmed if it's even possible!)
								log.Println("画像が見つかりません… 3秒後に再試行します… / No image found... trying again after 3 seconds...")
								time.Sleep(3 * time.Second)
								images[i].ID, err = SearchForImage(config, images[i].URL)
								log.Println("Image ID:", images[i].ID)
								if images[i].ID == "0" || err != nil {
									log.Println("画像の検索に失敗しました / Failed to search for image:", err)
									return err
								}
							}
						}
					}
					fmt.Printf("Images: %+v\n", images)
					err = createPostWithImage(config, feed.Items[0], images)
					if err != nil {
						log.Println("Misskeyの投稿をしくじりました... / Failed to post to Misskey:", err)
						return err
					} else {
						log.Println("Misskeyに投稿しました。: / Posted to Misskey:", feed.Items[0].Title)
						cache.saveLatestItem(newestItem)
					}
				} else {
					err = createPost(config, feed.Items[0])
					if err != nil {
						log.Println("Misskeyの投稿をしくじりました... / Failed to post to Misskey:", err)
						return err
					} else {
						log.Println("Misskeyに投稿しました。: / Posted to Misskey:", feed.Items[0].Title)
						cache.saveLatestItem(newestItem)
					}
				}
			}
		}
	}

	return nil
}

func getLatestItem(cache *Cache) string {

	return cache.getLatestItem()
}

func saveLatestItem(cache *Cache, id string) {

	cache.saveLatestItem(id)
}

func SearchForImage(config Config, imageURL string) (string, error) {

	// put md5 processing in its own helper function
	resp, err := http.Get(imageURL)
	if err != nil {
		return "0", err
	}
	defer resp.Body.Close()

	hash := md5.New()
	if _, err := io.Copy(hash, resp.Body); err != nil {
		return "0", err
	}
	hashInBytes := hash.Sum(nil)[:16]
	md5Hash := fmt.Sprintf("%x", hashInBytes)

	note := map[string]interface{}{
		"i":   config.AuthToken,
		"md5": md5Hash,
	}

	payload, err := json.Marshal(note)
	if err != nil {
		return "0", err
	}

	url := fmt.Sprintf("https://%s/api/drive/files/find-by-hash", config.MisskeyHost)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(payload))
	if err != nil {
		return "0", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", config.AuthToken)

	client := http.Client{}
	resp, err = client.Do(req)
	if err != nil {
		return "0", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "0", fmt.Errorf("(SearchForImage) MisskeyAPIと以下の理由で接続を確立できません / Failed to connect to Misskey API for the following reason: %d", resp.StatusCode)
	}

	var response []struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "0", err
	}

	if len(response) > 0 {
		return response[0].ID, nil
	}
	return "0", err
}

// TODO: validate image existance and delete if already exists
func UploadImage(config Config, imageURL string) error {
	note := map[string]interface{}{
		"i":   config.AuthToken,
		"url": imageURL,
		//"force":	  true, //uncomment if it does not change the image next post
		// then work on deduplication of images by finding the hash
	}

	payload, err := json.Marshal(note)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("https://%s/api/drive/files/upload-from-url", config.MisskeyHost)
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

	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("(UploadImage) 以下の理由でMisskeyへの画像アップロードに失敗しました / Failed to upload image to Misskey for the following reason: %d", resp.StatusCode)
	}

	return nil
}

// could this be a struct? research
func createPost(config Config, item *gofeed.Item) error {
	note := map[string]interface{}{
		"i":          config.AuthToken,
		"text":       item.Description,
		"visibility": "public",
	}
	return postToMisskey(config, note)
}

func createPostWithImage(config Config, item *gofeed.Item, images []Image) error {

	// process the image objects and convert to an array of strings so we can form the note
	var fileIds []string
	for _, image := range images {
		fileIds = append(fileIds, image.ID)
	}
	note := map[string]interface{}{
		"i":          config.AuthToken,
		"text":       item.Description,
		"visibility": "public",
		"fileIds":    fileIds,
	}
	return postToMisskey(config, note)
}

func postToMisskey(config Config, note map[string]interface{}) error {

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
		return fmt.Errorf("(PostToMisskey) MisskeyAPIと以下の理由で接続を確立できません / Failed to connect to Misskey API for the following reason: %d", resp.StatusCode)
	}

	return nil
}

func main() {
	fmt.Println("処理を開始しますっ！ / Starting process!")

	err := godotenv.Load()
	if err != nil {
		log.Println(".envファイルがありません...環境変数から読み込みを続行します。 / No .env file... moving on to loading from environment variables.", err)
	}

	var config Config
	err = envconfig.Process("", &config)
	if err != nil {
		log.Fatal("環境変数の読み込みをしくじりました...: / Failed to load environment variables:", err)
	}

	cache := &Cache{}

	//RSSを取得する間隔です。今回は結構頻繁に更新される事例を想定して短めに持たせているけど、NHKとかだと５分スパンで十分です。/ This is the interval for retrieving RSS. This time, it is set short assuming a case that is updated quite frequently, but for something like NHK, a 5-minute span is sufficient.
	//分数で指定する場合はtime.Minuteに書き換えてください。 / If specifying in minutes, change to time.Minute.
	interval := 5 * time.Minute
	ticker := time.NewTicker(interval)

	for {
		select {
		case <-ticker.C:
			log.Println("最新のRSS情報を取得しています / Retrieving the latest RSS information")
			errProcessRSS := processRSS(config, cache)
			if errProcessRSS != nil {
				log.Println("RSSの取得に失敗しました... / Failed to retrieve RSS:", errProcessRSS)
			}
			log.Println("最新のRSS情報を取得しました / Retrieved the latest RSS information")
		}
	}
}
