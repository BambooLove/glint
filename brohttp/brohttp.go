package brohttp

import (
	"context"
	"encoding/base64"
	"fmt"
	ast "glint/ast"
	"glint/config"
	"glint/logger"
	"glint/util"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

//Headers_IMPORTMENT
var headers_importment = []string{
	"Accept",
	"Content-Type",
	"Cookie",
	"Origin",
	"Referer",
	"Upgrade-Insecure-Requests",
	"User-Agent",
}

//Spider 爬虫资源，设计目的是基于浏览器发送payload，注意使用此结构的函数在多线程中没上锁是不安全的，理想状态为一条线程使用这个结构
type Spider struct {
	Ctx        *context.Context //存储着浏览器的资源
	Cancel     *context.CancelFunc
	TabTimeOut int64
}

type Tab struct {
	Ctx           *context.Context
	Cancel        *context.CancelFunc
	Responses     chan []map[string]string
	ReqMode       string
	PostData      []byte
	Standardlen   int //爬虫请求的长度
	ReqUrlresplen int
	Url           *url.URL
	Headers       map[string]interface{} //请求头
	Isreponse     bool
	Source        chan string //当前爬虫的html的源码
	RespDone      chan bool
	Reports       []ReportMsg
}

type ReportMsg struct {
	RequestID network.RequestID
	Count     int
}

type UrlOCC struct {
	Request ast.JsonUrl
	OCC     []ast.Occurence
}

func (spider *Spider) Close() {
	if spider.Cancel != nil && spider.Ctx != nil {
		defer (*spider.Cancel)()
		if (*spider.Ctx).Err() == nil {
			chromedp.Cancel(*spider.Ctx)
		} else {
			logger.Error("spider close call  fail error: %s", (*spider.Ctx).Err())
		}
	}
}

func (t *Tab) Close() {
	defer (*t.Cancel)()
	defer chromedp.Cancel(*t.Ctx)
}

func NewTab(spider *Spider) (*Tab, error) {
	var tab Tab
	ctx, cancel := chromedp.NewContext(*spider.Ctx)
	// logger.Info("set timeout for the tab page : %d second", 20)
	ctx, cancel = context.WithTimeout(ctx, time.Duration(300)*time.Second)
	tab.Ctx = &ctx
	tab.Cancel = &cancel
	tab.Responses = make(chan []map[string]string)
	tab.Source = make(chan string)
	tab.RespDone = make(chan bool)
	tab.ListenTarget()
	return &tab, nil
}

/*


func main() {
	// create context
	ctx, cancel := chromedp.NewContext(
		context.Background(),
		chromedp.WithLogf(log.Printf),
	)
	defer cancel()

	// create a timeout as a safety net to prevent any infinite wait loops
	ctx, cancel = context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	// set up a channel so we can block later while we monitor the download
	// progress
	done := make(chan bool)

	// set the download url as the chromedp github user avatar
	urlstr := "https://avatars.githubusercontent.com/u/33149672"

	// this will be used to capture the request id for matching network events
	var requestID network.RequestID

	// set up a listener to watch the network events and close the channel when
	// complete the request id matching is important both to filter out
	// unwanted network events and to reference the downloaded file later
	chromedp.ListenTarget(ctx, func(v interface{}) {
		switch ev := v.(type) {
		case *network.EventRequestWillBeSent:
			log.Printf("EventRequestWillBeSent: %v: %v", ev.RequestID, ev.Request.URL)
			if ev.Request.URL == urlstr {
				requestID = ev.RequestID
			}
		case *network.EventLoadingFinished:
			log.Printf("EventLoadingFinished: %v", ev.RequestID)
			if ev.RequestID == requestID {
				close(done)
			}
		}
	})

	// all we need to do here is navigate to the download url
	if err := chromedp.Run(ctx,
		chromedp.Navigate(urlstr),
	); err != nil {
		log.Fatal(err)
	}

	// This will block until the chromedp listener closes the channel
	<-done
	// get the downloaded bytes for the request id
	var buf []byte
	if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		buf, err = network.GetResponseBody(requestID).Do(ctx)
		return err
	})); err != nil {
		log.Fatal(err)
	}

	// write the file to disk - since we hold the bytes we dictate the name and
	// location
	if err := ioutil.WriteFile("download.png", buf, 0644); err != nil {
		log.Fatal(err)
	}
	log.Print("wrote download.png")
}
*/

func (t *Tab) ListenTarget() {
	//目前有个bug，go 关键字内就是不能用logger模块的日志输出结构体，使用后Listen内部会出现逻辑顺序错乱的情况，怀疑是logger里面的lock锁有关
	chromedp.ListenTarget(*t.Ctx, func(ev interface{}) {
		// var RequestID network.RequestID
		// logger.Info("%v", reflect.TypeOf(ev))
		switch ev := ev.(type) {
		case *page.EventLoadEventFired:
		case *runtime.EventConsoleAPICalled:
			// Response := make(map[string]string)
			// Responses := []map[string]string{}
			// logger.Debug("* console.%s call:\n", ev.Type)
			// for _, arg := range ev.Args {
			// 	fmt.Printf("%s - %s\n", arg.Type, string(arg.Value))
			// 	Response[string(ev.Type)] = strings.ReplaceAll(string(arg.Value), "\"", "")
			// 	Responses = append(Responses, Response)
			// }
			// go func() {
			// 	t.Responses <- Responses
			// }()
		case *runtime.EventExceptionThrown:
		case *fetch.EventRequestPaused:
			go func() {
				c := chromedp.FromContext(*t.Ctx)
				ctx := cdp.WithExecutor(*t.Ctx, c.Target)
				// var req *fetch.ContinueRequestParams
				select {
				case <-ctx.Done():
					return
				default:
				}

				req := fetch.ContinueRequest(ev.RequestID)
				// req.URL = spider.Url.String()
				req.Headers = []*fetch.HeaderEntry{}

				//设置文件头
				for key, value := range t.Headers {
					if value != nil {
						//这里只填写重要的header头
						for _, h := range headers_importment {
							if strings.EqualFold(h, key) {
								req.Headers = append(req.Headers, &fetch.HeaderEntry{Name: key, Value: value.(string)})
							}
						}
					}
				}

				if t.ReqMode == "POST" {
					req.Method = "POST"
					req.PostData = base64.StdEncoding.EncodeToString(t.PostData)
				}

				if err := req.Do(ctx); err != nil {
					logger.Printf("fetch.EventRequestPaused Failed to continue request: %v", err)
				}
			}()
		case *network.EventRequestWillBeSent:
			//fmt.Println(aurora.Sprintf("EventRequestWillBeSent==>  url: %s requestid: %s", aurora.Red(ev.Request.URL), aurora.Red(ev.RequestID)))
			//重定向
			request := ev
			if ev.RedirectResponse != nil {
				logger.Debug("链接 %s: 重定向到: %s", request.RedirectResponse.URL, request.DocumentURL)
			}

			// t.Report.RequestID = ev.RequestID

			// if ev.Request.URL == urlstr {
			// 	RequestID = ev.RequestID
			// }

		case *network.EventLoadingFinished:

			// go func(RequestID network.RequestID) {
			// 	c := chromedp.FromContext(*t.Ctx)
			// 	ctx := cdp.WithExecutor(*t.Ctx, c.Target)
			// 	data, e := network.GetResponseBody(RequestID).Do(ctx)
			// 	// }
			// 	if e != nil {
			// 		fmt.Printf("network.EventResponseReceived error: %v", e)
			// 		return
			// 	}
			// 	if len(data) > 0 {
			// 		t.Source <- string(data)
			// 	}
			// }(RequestID)

		case *network.EventResponseReceived:

		case *page.EventJavascriptDialogOpening:
			logger.Debug("* EventJavascriptDialogOpening.%s call", ev.Type)
			// Response[string(ev.Type)] = strings.ReplaceAll(ev.Message, "\"", "")
			// Responses = append(Responses, Response)
			go func() {
				c := chromedp.FromContext(*t.Ctx)
				ctx := cdp.WithExecutor(*t.Ctx, c.Target)
				//关闭弹窗
				page.HandleJavaScriptDialog(false).Do(ctx)
				// t.Responses <- Responses
			}()
		}
	})
}

func (spider *Spider) Init(TaskConfig config.TaskConfig) error {
	options := []chromedp.ExecAllocatorOption{
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("disable-web-security", true),
		chromedp.Flag("disable-xss-auditor", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-setuid-sandbox", true),
		chromedp.Flag("ignore-certificate-errors", true),
		chromedp.Flag("allow-running-insecure-content", true),
		chromedp.Flag("disable-webgl", true),
		chromedp.Flag("disable-popup-blocking", true),
		chromedp.Flag("blink-settings", "imagesEnabled=false"),
		chromedp.UserAgent(`Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/94.0.4606.81 Safari/537.36`),
	}
	options = append(chromedp.DefaultExecAllocatorOptions[:], options...)

	if TaskConfig.Proxy != "" {
		options = append(options, chromedp.Flag("proxy-server", TaskConfig.Proxy))
	}

	c, cancel := chromedp.NewExecAllocator(context.Background(), options...)
	ctx, cancel := chromedp.NewContext(c) // chromedp.WithDebugf(logger.Info)
	spider.Cancel = &cancel
	spider.Ctx = &ctx
	spider.TabTimeOut = int64(TaskConfig.TabRunTimeout * time.Second)
	err := chromedp.Run(
		*spider.Ctx,
		fetch.Enable(),
	)

	return err
}

//Sendreq 发送请求 url为空使用爬虫装载的url
func (t *Tab) Send() ([]string, error) {
	var htmls []string
	var res string
	err := chromedp.Run(
		*t.Ctx,
		fetch.Enable(),
		chromedp.Navigate(t.Url.String()),
		chromedp.OuterHTML("html", &res, chromedp.ByQuery),
	)
	if err != nil {
		logger.Error(err.Error())
	}

	htmls = append(htmls, res)
	//循环两次获取,不会获取过多内容
	// for i := 0; i < 2; i++ {
	// 	select {
	// 	case html := <-t.Source:
	// 		htmls = append(htmls, html)
	// 	case <-time.After(time.Second):
	// 	}
	// }
	//logger.Info("%v", htmls)
	// res = html.UnescapeString(res)
	return htmls, err
}

func (t *Tab) GetRequrlparam() (url.Values, error) {
	if len(t.Url.String()) == 0 {
		panic("request url is emtry")
	}
	u, err := url.Parse(t.Url.String())
	if err != nil {
		panic(err)
	}
	m, err := url.ParseQuery(u.RawQuery)
	return m, err
}

//GetReqLensByHtml 二度获取请求的长度
func (t *Tab) GetReqLensByHtml(JsonUrls *ast.JsonUrl) error {
	if len(t.Url.String()) == 0 {
		panic("request url is emtry")
	}

	if JsonUrls.MetHod == "GET" {
		t.ReqMode = "GET"
		t.Url, _ = url.Parse(JsonUrls.Url)
		response, err := t.Send()
		if err != nil {
			return err
		}
		t.Standardlen = len(response)
	} else {
		t.ReqMode = "POST"
		t.Url, _ = url.Parse(JsonUrls.Url)
		t.PostData = []byte(JsonUrls.Data)
		response, err := t.Send()
		if err != nil {
			return err
		}
		t.Standardlen = len(response)
	}

	return nil
}

//BuildPayload words和 extension 是映射关系
type BuildPayload struct {
	i     int
	value string
	words []string
}

func (g *BuildPayload) Next() bool {
	if g.i == len(g.words) {
		return false
	}
	g.value = g.words[g.i]
	g.i++
	return true
}

func (g *BuildPayload) Value() interface{} {
	return g.value
}

//GetPayloadValue 迭代 payload
func (g *BuildPayload) GetPayloadValue() (string, error) {
	if g.Next() {
		switch v := g.Value().(type) {
		case string:
			return v, nil
		}
	}
	return "", fmt.Errorf("the datas is nothing")
}

//PayloadHandle payload处理,把payload根据请求方式的不同修改 paramname
func (t *Tab) PayloadHandle(payload string, reqmod string, paramname string, Getparams url.Values) error {
	t.ReqMode = reqmod

	if reqmod == "GET" {
		if len(Getparams) == 0 {
			return fmt.Errorf("GET参数为空")
		}
		payloads := []string{payload}
		Getparams[paramname] = payloads
		t.Url.RawQuery = Getparams.Encode()
	} else {
		if len(t.PostData) == 0 {
			return fmt.Errorf("POST参数为空")
		}
		t.PostData = []byte(payload)
	}
	return nil
}

//这个要改一下加速发包速度
func (t *Tab) CheckPayloadLocation(newpayload string) ([]string, error) {
	var (
		htmls []string
	)

	if t.ReqMode == "GET" {
		Getparams, err := t.GetRequrlparam()
		tmpParams := make(url.Values)
		for key, value := range Getparams {
			tmpParams[key] = value
		}
		if err != nil {
			logger.Error(err.Error())
		}
		if t.Headers["Referer"] == t.Url.String() {
			html_s, err := t.Send()
			if err != nil {
				return nil, err
			}
			htmls = append(htmls, html_s...)
		} else {

			for param, _ := range Getparams {
				t.PayloadHandle(newpayload, "GET", param, Getparams)
				Getparams = tmpParams
				html_s, err := t.Send()
				if err != nil {
					return nil, err
				}
				htmls = append(htmls, html_s...)
			}
		}

		if len(Getparams) == 0 {
			html_s, err := t.Send()
			if err != nil {
				return nil, err
			}
			htmls = append(htmls, html_s...)
		}
		return htmls, nil
	} else {
		PostData := t.PostData
		if value, ok := t.Headers["Content-Type"]; ok {
			params, err := util.ParseUri("", PostData, "POST", value.(string))
			if err != nil {
				logger.Error(err.Error())
				return nil, err
			}
			payloads := params.SetPayload("", newpayload, "POST")
			for _, v := range payloads {
				t.PostData = []byte(PostData)
				t.PayloadHandle(v, "POST", "", nil)
				html_s, err := t.Send()
				if err != nil {
					return nil, err
				}
				htmls = append(htmls, html_s...)
			}

		} else {
			logger.Error("checkpayloadlocation error: haven't found content type")
		}
		return htmls, nil
	}
}

func (t *Tab) CheckRandOnHtmlS(playload string, urlrequst interface{}) (bool, map[int]interface{}) {
	var urlocc UrlOCC
	ReponseInfo := make(map[int]interface{})
	htmls, _ := t.CheckPayloadLocation(playload)
	var bOnhtml bool = false
	for i, html := range htmls {
		Node := ast.SearchInputInResponse(playload, html)
		if len(Node) != 0 {
			bOnhtml = true
		}
		//重置Url参数
		t.CopyRequest(urlrequst)
		urlocc.Request = t.ReqtoJson()
		urlocc.OCC = Node
		ReponseInfo[i] = urlocc
	}
	return bOnhtml, ReponseInfo
}

func (t *Tab) CopyRequest(data interface{}) {
	var lock sync.Mutex
	lock.Lock()
	defer lock.Unlock()
	switch v := data.(type) {
	case map[string]interface{}:
		t.ReqMode = v["method"].(string)
		t.Url, _ = url.Parse(v["url"].(string))
		t.PostData = []byte(v["data"].(string))
		t.Headers = v["headers"].(map[string]interface{})
	case ast.JsonUrl:
		t.ReqMode = v.MetHod
		t.Url, _ = url.Parse(v.Url)
		t.PostData = []byte(v.Data)
		t.Headers = v.Headers
	}
}

func (t *Tab) ReqtoJson() ast.JsonUrl {
	var data ast.JsonUrl
	data.MetHod = t.ReqMode
	data.Url = t.Url.String()
	data.Data = string(t.PostData)
	data.Headers = t.Headers
	return data
}
