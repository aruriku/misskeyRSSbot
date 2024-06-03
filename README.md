### Disclaimer: 
This fork has specific changes to suite a specific use. For general use, please use the original instead.


# Misskey RSS BOT
A simple BOT tool to post the latest news obtained via RSS to Misskey🐈‍⬛💻

## Usage

1.Create a `.env` file in the root directory and write the following as shown `.env.example`.

2.`go build` or `go run main.go`


## Deploy

You can use tmux or systemd to run the program in the background.

You can also use docker and docker-compose using the included dockerfile and example docker-compose.yml file.

To run the container, run `docker-compose up -d`. This will run the bot as a daemon in the background.
You may need to build the image. If so, just add `--build` to your command.


You can use a `.env` file to load your settings or set them as environment variables. The .env file takes precedence.


## Option

If you want to use multi URL, please modify as this 

```dotenv
RSS_URL:"https://example.com/rss/news/cat0.xml,https://example.com/rss/news/cat1.xml,https://example.com/rss/news/cat2.xml"
```
