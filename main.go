package main

import (
	"context"
	_ "embed"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/cixtor/readability"
	"github.com/mmcdole/gofeed"
)

var (
	feeds = []string{
		/* some of my fav*/
		"https://robertleggett.blog/feed/",
		"https://jameshfisher.com/feed.xml",
		"https://jvns.ca/atom.xml",
		"https://blog.boot.dev/index.xml",
		"https://ops.tips/index.xml",

		// TODO : check
		"https://themargins.substack.com/feed.xml",
		"https://joy.recurse.com/feed.atom",
		"https://danluu.com/atom.xml",
		"https://blog.veitheller.de/feed.rss",
		"https://twobithistory.org/feed.xml",
		"https://rachelbythebay.com/w/atom.xml",
		"https://scattered-thoughts.net/rss.xml",

		"https://gochugarugirl.com/feed/",

		"https://commoncog.com/blog/rss/",
		"https://highgrowthengineering.substack.com/feed",
		"http://tonsky.me/blog/atom.xml",
		"https://www.benkuhn.net/index.xml",

		"https://hnrss.org/frontpage?points=50",
		"https://solar.lowtechmagazine.com/feeds/all-en.atom.xml",
		"https://www.slowernews.com/rss.xml",
		"https://macwright.com/rss.xml",
		"https://mikehudack.substack.com/feed",
		"https://www.wildlondon.org.uk/blog/all/rss.xml",
		"https://blaggregator.recurse.com/atom.xml?token=4c4c4e40044244aab4a36e681dfb8fb0",

		"https://blog.golang.org/feed.atom?format=xml",
		"https://anewsletter.alisoneroman.com/feed",

		"https://routley.io/reserialised/great-expectations/2022-08-24/index.xml",
	}
	wg         sync.WaitGroup
	outputDir  = "docs" // So we can host the site on GitHub Pages
	outputFile = "index.html"
	logFile    = path.Join(outputDir, "log.txt")
)

//go:embed styles.css
var styles string

//go:embed post.template.html
var postTmpl string

//go:embed index.template.html
var indexTmpl string

func main() {
	timeout := time.Minute
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// creating directory
	if err := os.MkdirAll(outputDir, 0700); err != nil {
		log.Printf("Unable to create directory : %v", err)
		return
	}
	var mu sync.Mutex
	//postChan := make(chan *Post)
	var posts []*Post

	wg.Add(len(feeds))
	for _, feed := range feeds {
		go getPost(ctx, feed, &posts, &mu)
	}
	seen := map[string]bool{}
	//go func() {
	//	for post := range postChan {
	//		if seen[post.Link] {
	//			continue
	//		}
	//		posts = append(posts, post)
	//		seen[post.Link] = true
	//	}
	//}()

	go func() {
		for _, post := range posts {
			if seen[post.Link] {
				continue
			}
			posts = append(posts, post)
			seen[post.Link] = true
		}
	}()

	wg.Wait()
	//close(postChan)
	if err := os.RemoveAll(path.Join(outputDir, "posts")); err != nil {
		log.Printf("Unable to create directory : %v", err)
		return
	}
	if err := os.MkdirAll(path.Join(outputDir, "posts"), 0700); err != nil {
		log.Printf("Unable to create directory : %v", err)
		return
	}
	for _, post := range posts {
		if post.Content == "" {
			log.Printf("No Content Present ")
			continue
		}

		f, err := os.Create(path.Join(outputDir, "posts", post.Filename))

		fmt.Printf(f.Name())
		if err != nil {
			log.Printf("error while creating file inside posts for link %v and error is %v", post.Link, err)
			return
		}

		temp, err := template.New("post").Parse(postTmpl)
		if err != nil {
			log.Printf("error while template parsing posts for link %v and error is %v", post.Link, err)
			return
		}

		data := map[string]interface{}{
			"Styles":   template.CSS(styles),
			"Content":  post.Content,
			"Original": post.Link,
			"Title":    post.Title,
		}
		if err := temp.Execute(f, data); err != nil {
			log.Printf("error while template execution posts for link %v and error is %v", post.Link, err)
			return
		}
		f.Close()
	}
	f, err := os.Create(path.Join(outputDir, outputFile))
	if err != nil {
		log.Printf("error while creating file inside posts and error is %v", err)
		return
	}
	defer f.Close()

	data := map[string]interface{}{
		"Posts":  posts,
		"Styles": template.CSS(styles),
	}
	if err := executeTemplate(f, data); err != nil {
		log.Printf("error while template execution posts and error is %v", err)
		return
	}

}

func executeTemplate(writer io.Writer, templateData map[string]interface{}) error {
	tmpl, err := template.New("index").Parse(indexTmpl)
	if err != nil {
		return err
	}
	if err := tmpl.Execute(writer, templateData); err != nil {
		return err
	}

	return nil
}

type Post struct {
	Title     string
	Link      string
	Published time.Time
	Host      string
	Content   template.HTML
	Filename  string
}

func getPost(ctx context.Context, feedurl string, posts *[]*Post, mu *sync.Mutex) {
	parser := gofeed.NewParser()
	defer wg.Done()
	//feeds, err := parser.ParseURL(url)
	feeds, err := parser.ParseURLWithContext(feedurl, ctx)
	if err != nil {
		log.Println(fmt.Errorf("error parsing %s: %w", feedurl, err))
		return
	}
	for _, feed := range feeds.Items {
		publishedDate := feed.PublishedParsed
		if publishedDate == nil {
			publishedDate = feed.UpdatedParsed
		}
		// you can use it for blocking any website/ domain
		parsedUrl, err := url.Parse(feed.Link)
		if err != nil {
			log.Println(fmt.Errorf("error parsing link: %s: %w", feedurl, err))
			return
		}

		feedpost := &Post{
			Title:     feed.Title,
			Link:      feed.Link,
			Published: *publishedDate,
			Host:      parsedUrl.Host,
			Content:   template.HTML(feed.Content),
			Filename:  structureFilename(feed.Title),
		}
		if feedpost.Content == "" {
			req, err := http.NewRequestWithContext(ctx, "GET", feed.Link, nil)
			if err != nil {
				log.Println(err)
				continue
			}
			req.Header["User-Agent"] = []string{
				"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
			}
			res, err := http.DefaultClient.Do(req)
			if err != nil {
				log.Println(err)
				continue
			}

			contentType := res.Header.Get("content-type")
			if contentType != "" && !strings.HasPrefix(contentType, "text/html") {
				log.Printf("Not HTML: %s\n", feed.Link)
				continue
			}
			postContent, err := readability.New().Parse(res.Body, feed.Link)
			if err != nil {
				log.Println(err)
				continue
			}

			feedpost.Content = template.HTML(postContent.Content)
			if feedpost.Content == "" {
				log.Printf("Content still empty after HTML reader: %s", feed.Link)
			}

			defer res.Body.Close()
		}
		mu.Lock()
		*posts = append(*posts, feedpost)
		mu.Unlock()
		//postChan <- feedpost
	}
	return
}

func structureFilename(title string) string {
	title = regexp.MustCompile(`([^\w\d])`).ReplaceAllLiteralString(title, " ")
	title = strings.ToLower(title)
	fields := strings.Fields(title)
	title = strings.Join(fields, "-") + ".html"
	title = strings.TrimPrefix(title, "show-hn-")
	title = strings.TrimPrefix(title, "ask-hn-")
	return title
}
