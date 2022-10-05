/*
Copyright © 2022 NAME HERE <EMAIL ADDRESS>
*/
package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/antchfx/htmlquery"
	"github.com/robertkrimen/otto"
	"github.com/spf13/cobra"
	"github.com/xue0228/xspider"
	"go.uber.org/zap/zapcore"
)

var (
	startIndex    int
	saveDir       string
	logEnabled    bool
	downloadDelay int
)

func parse(r *xspider.Response, s *xspider.Spider) xspider.RequestItems {
	doc, err := htmlquery.Parse(bytes.NewBuffer(r.Body))
	if err != nil {
		s.Log.Errorw("解析网页失败", "error", err)
		return xspider.RequestItems{}
	}

	info := htmlquery.Find(doc, `//div[@class="info"]`)[0]
	tem := htmlquery.FindOne(info, `/p[@class="title"]`)
	title := htmlquery.OutputHTML(tem, false)
	title = strings.Split(title, `<span class="right">`)[0]
	title = strings.Trim(title, " ")
	tem2 := htmlquery.Find(info, `/p[@class="subtitle"]/a`)
	authors := []string{}
	for _, v := range tem2 {
		authors = append(authors, strings.Trim(htmlquery.InnerText(v), " "))
	}
	author := strings.Join(authors, "_")
	tem3 := htmlquery.FindOne(info, `/p[@class="tip"]/span/span`)
	stats := htmlquery.InnerText(tem3)
	tem4 := htmlquery.FindOne(info, `/p[@class="tip"]/span/a/span`)
	theme := htmlquery.InnerText(tem4)
	comic := bytes.NewBufferString("【")
	comic.WriteString(stats)
	comic.WriteString("】【")
	comic.WriteString(theme)
	comic.WriteString("】")
	comic.WriteString(title)
	comic.WriteString("_")
	comic.WriteString(author)

	list := htmlquery.Find(doc, `//ul[@id="detail-list-select-1"]//a[@href]`)
	target := list[len(list)-startIndex]
	href := htmlquery.SelectAttr(target, "href")
	url := "https://tel.dm5.com" + href
	request, _ := xspider.NewRequest("GET", url, nil)
	request.Callback = parseChapter
	request.Ctx.Put("comic", comic.String())
	return xspider.RequestItems{request}
}

func parseChapter(r *xspider.Response, s *xspider.Spider) xspider.RequestItems {
	doc, err := htmlquery.Parse(bytes.NewBuffer(r.Body))
	if err != nil {
		s.Log.Errorw("解析网页失败", "error", err)
		return xspider.RequestItems{}
	}

	title := htmlquery.FindOne(doc, `//div[@class="title"]/span[@class="active right-arrow"]`)
	chapter := htmlquery.InnerText(title)
	chapter = strings.Trim(chapter, " ")

	container := htmlquery.FindOne(doc, `//div[@class="view-paging"]/div[@class="container"]`)
	page_num, _ := strconv.Atoi(xspider.FindScriptVar(r.Body, "DM5_IMAGE_COUNT"))

	tem2 := htmlquery.Find(container, `/a`)
	res := xspider.RequestItems{}
	target := tem2[len(tem2)-1]
	if htmlquery.InnerText(target) == "下一章" {
		href := htmlquery.SelectAttr(target, "href")
		url := "https://tel.dm5.com" + href
		request, _ := xspider.NewRequest("GET", url, nil)
		request.Ctx.Put("comic", r.Ctx.GetString("comic"))
		request.Callback = parseChapter
		res = append(res, request)
	}
	url := r.Request.Url.String()
	url = url[:len(url)-1]
	for i := 1; i <= page_num; i++ {
		request, _ := xspider.NewRequest("GET", url+fmt.Sprintf("-p%d/", i), nil)
		request.Ctx.Put("comic", r.Ctx.GetString("comic"))
		request.Ctx.Put("chapter", chapter)
		request.Ctx.Put("index", i)
		request.Callback = parsePage
		res = append(res, request)
	}
	return res
}

func parsePage(r *xspider.Response, s *xspider.Spider) xspider.RequestItems {
	mid := xspider.FindScriptVar(r.Body, "DM5_MID")
	cid := xspider.FindScriptVar(r.Body, "DM5_CID")
	dt := xspider.FindScriptVar(r.Body, "DM5_VIEWSIGN_DT")
	sign := xspider.FindScriptVar(r.Body, "DM5_VIEWSIGN")
	page := r.Ctx.GetInt("index")
	key := ""
	language := 1
	gtk := 6
	params := url.Values{}
	params.Add("_mid", mid)
	params.Add("cid", cid)
	params.Add("page", fmt.Sprintf("%d", page))
	params.Add("key", key)
	params.Add("language", fmt.Sprintf("%d", language))
	params.Add("gtk", fmt.Sprintf("%d", gtk))
	params.Add("_cid", cid)
	params.Add("_dt", dt)
	params.Add("_sign", sign)
	url := r.Request.Url.String() + "chapterfun.ashx?" + params.Encode()
	request, _ := xspider.NewRequest("GET", url, nil)
	request.Headers.Add("referer", r.Request.Url.String())
	request.Ctx.Put("comic", r.Ctx.GetString("comic"))
	request.Ctx.Put("chapter", r.Ctx.GetString("chapter"))
	request.Ctx.Put("index", r.Ctx.GetInt("index"))
	request.Ctx.Put("referer", r.Request.Url.String())
	request.Callback = parseImagePath

	return xspider.RequestItems{request}
}

func parseImagePath(r *xspider.Response, s *xspider.Spider) xspider.RequestItems {
	command := xspider.BytesToString(r.Body)
	vm := otto.New()
	vm.Run(command)
	v, err := vm.Get("d")
	if err != nil {
		xspider.NewResponseLogger(s.Log, r).Errorw("js解析失败", "error", err)
		return xspider.RequestItems{}
	}
	url := strings.Split(v.String(), ",")[0]

	request, _ := xspider.NewRequest("GET", url, nil)
	request.Headers.Add("referer", r.Ctx.GetString("referer"))
	request.Ctx.Put("comic", r.Ctx.GetString("comic"))
	request.Ctx.Put("chapter", r.Ctx.GetString("chapter"))
	request.Ctx.Put("index", r.Ctx.GetInt("index"))
	request.Callback = parseImage
	return xspider.RequestItems{request}
}

func parseImage(r *xspider.Response, s *xspider.Spider) xspider.RequestItems {
	contentType := r.Headers.Get("content-type")
	if contentType[:6] == "image/" {
		ext := contentType[6:]
		filename := filepath.Join(saveDir, r.Ctx.GetString("comic"), r.Ctx.GetString("chapter"), fmt.Sprintf("%04d.%s", r.Ctx.GetInt("index"), ext))
		r.Save(filename)
	}
	return xspider.RequestItems{}
}

func run(comicUrls []string) {
	_, err := os.Stat(saveDir)
	if os.IsNotExist(err) {
		panic(errors.New("文件夹不存在"))
	}

	spider := xspider.NewSpider(
		xspider.StartUrls(comicUrls),
		xspider.DefaultParseFunc(parse),
		xspider.Settings(xspider.NewSetting(
			xspider.LogLevel(zapcore.InfoLevel), xspider.DownloadDelay(time.Millisecond*time.Duration(downloadDelay)), xspider.DepthLimit(0),
			// xspider.LogFile("info.log"), xspider.ErrFile("err.log"),
		)),
	)
	if logEnabled {
		spider.Settings.LogFile = filepath.Join(saveDir, "info.log")
		spider.Settings.ErrFile = filepath.Join(saveDir, "err.log")
	}

	spider.Run()
}

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "dm5 [-s save_dir] [-d download_delay] [-i start_index] url...",
	Short: "下载https://tel.dm5.com/网站中的免费漫画",
	Long:  `提供需要下载漫画的详情页网址以及期望的起始下载章节即可快速完成漫画的下载`,
	// Uncomment the following line if your bare application
	// has an action associated with it:
	Args: cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		run(args)
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	// rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.x-spider.yaml)")

	// Cobra also supports local flags, which will only run
	// when this action is called directly.
	// rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	rootCmd.Flags().StringVarP(&saveDir, "savedir", "s", ".", "保存漫画的文件夹地址")
	rootCmd.Flags().BoolVarP(&logEnabled, "log", "l", false, "保存记录文件到savedir指定的文件夹")
	rootCmd.Flags().IntVarP(&downloadDelay, "delay", "d", 0, "每次下载漫画图片的网络请求间隔")
	rootCmd.Flags().IntVarP(&startIndex, "index", "i", 1, "指定漫画下载的起始章节，从1开始计数")
}
