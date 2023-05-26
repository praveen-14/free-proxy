package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/ecarter202/scrapingbee-go"
	"github.com/praveen-14/user-manager/services/logger"
)

type (
	CheckFnType = func(*http.Response, error) (bool, error)

	Config struct {
		SbApiKey                string
		ProxySearchThreadLimit  int
		ProxySearcNumMaxRetries int
		ProxyChanSize           int
		WorkingProxyChanSize    int
		Debug                   bool
	}

	ProxyServer struct {
		Config
		sbClient         *scrapingbee.Client
		proxyChan        chan Proxy
		workingProxyChan chan Proxy
	}
)

func New(config Config) (*ProxyServer, context.CancelFunc) {
	server := ProxyServer{}
	server.Config = config
	server.sbClient = scrapingbee.New(config.SbApiKey)
	server.proxyChan = make(chan Proxy, config.ProxyChanSize)
	server.workingProxyChan = make(chan Proxy, config.WorkingProxyChanSize)

	ctx, cancel := context.WithCancel(context.Background())
	// proxyChan = proxy.StartProxyFiltering(ctx, proxy.StartProxySearch(ctx))
	server.StartProxySearch(ctx)

	return &server, cancel
}

var (
	log                   = logger.New("proxy", 0)
	ErrMaxRetriesExceeded = fmt.Errorf("retry limit exceeded. Possibly url is not reachable given maxRetires is set to a large value like 512 or 1024")
	ErrGaveUp             = fmt.Errorf("Give up criteria met")
)

func (server *ProxyServer) LoadDoc(link string, isBlockedFn, giveUpFn CheckFnType, threadLimit, numMaxRetries int, allowRedirects bool) (doc *goquery.Document, res *http.Response, err error) {
	for {
		res, err = server.Get(link, threadLimit, numMaxRetries, giveUpFn, allowRedirects)
		if res != nil {
			defer res.Body.Close()
		}
		if err != nil {
			switch err {
			case ErrGaveUp:
				if server.Config.Debug {
					log.Print("FAIL", "url retrieving using proxy halted due to meeting give up criteria [%s] [ERR: %s]", link, err)
				}
			case ErrMaxRetriesExceeded:
				if server.Config.Debug {
					log.Print("FAIL", "url retrieving using proxy halted due to exceeding max retries [%s] [ERR: %s]", link, err)
				}
			default:
				if server.Config.Debug {
					log.Print("FAIL", "url retrieving using proxy halted due to an unexpected error [%s] [ERR: %s]", link, err)
				}
			}
			break
		}

		doc, err = goquery.NewDocumentFromReader(res.Body)
		if err != nil {
			if server.Config.Debug {
				log.Print("FAIL", "retrying... reading response body failed with [%s] [ERR: %s]", link, err)
			}
			continue
		}

		blocked, err := isBlockedFn(res, err)
		if err != nil {
			if server.Config.Debug {
				log.Print("FAIL", "isBlockedFn failed [%s] [ERR: %s]", link, err)
			}
			break
		}
		if blocked {
			continue
		}

		break
	}

	// scraping bee
	if err != nil {
		if server.Config.Debug {
			log.Print("INFO", "failed free proxies, trying SB %s", link)
		}
		res, err = server.sbClient.Get(link, nil)
		if res != nil {
			defer res.Body.Close()
		}
		if err != nil {
			if server.Config.Debug {
				log.Print("FAIL", "failed scraping-bee get %s", err)
			}
			return nil, nil, err
		}
		bodyBytes, _ := ioutil.ReadAll(res.Body)
		res.Body = io.NopCloser(ReusableReader(bytes.NewBuffer(bodyBytes)))

		doc, err = goquery.NewDocumentFromResponse(res)
		if err != nil {
			if server.Config.Debug {
				fmt.Print("FAIL", "cannot create doc, [ERR=%s]", err)
			}
			return nil, nil, err
		}

		resolvedURL, ok := res.Header["Spb-Resolved-Url"]
		if !ok || len(resolvedURL) == 0 {
			errMsg := fmt.Sprintf("Spb-Resolved-Url header not returned from scraping bee %s [ERR: %s] [code: %d]", link, err, res.StatusCode)
			if server.Config.Debug {
				log.Print("FAIL", "%s", errMsg)
			}
			return nil, nil, fmt.Errorf(errMsg)
		}
		res.Request.URL, err = url.Parse(resolvedURL[0])
		if err != nil {
			if server.Config.Debug {
				log.Print("FAIL", "failed to parse returned Spb-Resolved-Url returned from scraping bee %s [ERR: %s]", link, err)
			}
			return nil, nil, err
		}
	}

	if err != nil {
		doc = nil
		res = nil
	}

	return doc, res, err
}

func (server *ProxyServer) Get(link string, threadLimit int, maxRetries int, giveUpFn CheckFnType, allowRedirects bool) (out *http.Response, outErr error) {

	wg := new(sync.WaitGroup)
	retries := 0
	lock := &sync.Mutex{}

	for i := 0; i < threadLimit; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				lock.Lock()
				retries += 1
				if out != nil {
					lock.Unlock()
					return
				}
				if retries > maxRetries {
					outErr = ErrMaxRetriesExceeded
					lock.Unlock()
					return
				}
				lock.Unlock()

				var proxy Proxy
				select {
				case proxy = <-server.workingProxyChan:
				default:
					select {
					case proxy = <-server.proxyChan:
					default:
						proxy = Proxy{} // a proxy will not be used
					}
				}

				myClient := &http.Client{
					Timeout: 5 * time.Second,
				}
				if allowRedirects {
					myClient.CheckRedirect = func(req *http.Request, via []*http.Request) error {
						return http.ErrUseLastResponse
					}
				}

				if proxy.IP != "" {
					proxyUrl, err := url.Parse(fmt.Sprintf("http://%s:%s", proxy.IP, proxy.Port)) //    http:// prefix works for even https proxies
					if err != nil {
						continue
					}
					myClient.Transport = &http.Transport{Proxy: http.ProxyURL(proxyUrl)}
				}

				req, _ := http.NewRequest("GET", link, nil)
				req.Close = true
				req.Header.Set("Connection", "close")
				req.Header.Set(`user-agent`, `Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/107.0.0.0 Safari/537.36`)

				res, err := myClient.Do(req)
				if res != nil {
					defer res.Body.Close()
				}

				// replace body with a reusable reader
				if err == nil {
					bodyBytes, bodyReadErr := ioutil.ReadAll(res.Body)
					//  must close
					res.Body = io.NopCloser(ReusableReader(bytes.NewBuffer(bodyBytes)))

					if bodyReadErr != nil {
						if server.Config.Debug {
							log.Print("FAIL", "bodyReadErr %s", bodyReadErr)
						}
						continue
					}
				}

				giveUp, giveUperr := giveUpFn(res, err)
				if giveUperr != nil {
					lock.Lock()
					outErr = giveUperr
					lock.Unlock()
					if server.Config.Debug {
						log.Print("FAIL", "failed checking giveUpFn %s", err)
					}
					return
				}
				if giveUp {
					lock.Lock()
					outErr = ErrGaveUp
					lock.Unlock()
					return
				}

				if err != nil {
					if server.Config.Debug {
						log.Print("FAIL", "http %s", err)
					}
					continue
				}

				if res.StatusCode == 200 {
					server.workingProxyChan <- proxy
					lock.Lock()
					out = res
					lock.Unlock()
					return
				}

				if server.Config.Debug {
					log.Print("FAIL", "other %d %s", res.StatusCode, link)
				}
			}
		}()
	}

	wg.Wait()

	// since out and err is read/written by mutiple go routines, they can have contradicting values
	if out != nil {
		outErr = nil
	}
	if server.Config.Debug {
		log.Print("INFO", "proxy search finished for %s, successful? %t", link, out != nil)
	}
	return out, outErr
}

// START - REUSABLE READER

type reusableReader struct {
	io.Reader
	readBuf *bytes.Buffer
	backBuf *bytes.Buffer
}

func ReusableReader(r io.Reader) io.Reader {
	readBuf := bytes.Buffer{}
	readBuf.ReadFrom(r) // error handling ignored for brevity
	backBuf := bytes.Buffer{}

	return reusableReader{
		io.TeeReader(&readBuf, &backBuf),
		&readBuf,
		&backBuf,
	}
}

func (r reusableReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	if err == io.EOF {
		r.reset()
	}
	return n, err
}

func (r reusableReader) reset() {
	io.Copy(r.readBuf, r.backBuf)
}

// func Get(link string, proxyChan chan Proxy, workingProxyChan chan Proxy) (*http.Response, error) {
// 	for {
// 		var proxy Proxy
// 		select {
// 		case proxy = <-workingProxyChan:
// 		default:
// 			proxy = <-proxyChan
// 		}

// 		proxyUrl, err := url.Parse(fmt.Sprintf("http://%s:%s", proxy.IP, proxy.Port)) //    http:// prefix works for even https proxies
// 		if err != nil {
// 			return nil, err
// 		}
// 		myClient := &http.Client{
// 			Transport: &http.Transport{Proxy: http.ProxyURL(proxyUrl)},
// 			Timeout:   5 * time.Second,
// 		}

// 		res, err := myClient.Get(link)

// 		if err == nil && res.StatusCode == 200 {
// 			log.Print("SUCCESS", "GET success %d %s %s", res.StatusCode, proxy, link)
// 			workingProxyChan <- proxy
// 			return res, nil
// 		}
// 		log.Print("FAIL", "GET failed %s %s %s", proxy, link, err)
// 	}
// }

// func Get(link string, proxies []Proxy) (*http.Response, error) {
// 	ctx, cancel := context.WithCancel(context.Background())
// 	// defer cancel()

// 	// var res *http.Response
// 	// resLock := &sync.Mutex{}
// 	// wg := new(sync.WaitGroup)

// 	return nil, nil

// 	// proxies, _ := GetUSProxies(ctx)

// 	// for i := 0; i < reqConcurrency; i++ {
// 	// 	wg.Add(1)

// 	// 	go func() {
// 	// 		defer wg.Done()
// 	// 		for {
// 	// 			select {
// 	// 			case <-ctx.Done():
// 	// 				return
// 	// 			default:
// 	// 				proxy, ok := <-proxyChan
// 	// 				if ok {
// 	// 					proxyUrl, _ := url.Parse(fmt.Sprintf("http://%s:%s", proxy.IP, proxy.Port))
// 	// 					if proxy.Https == "yes" {
// 	// 						proxyUrl, _ = url.Parse(fmt.Sprintf("https://%s:%s", proxy.IP, proxy.Port))
// 	// 					}
// 	// 					myClient := &http.Client{
// 	// 						Transport: &http.Transport{Proxy: http.ProxyURL(proxyUrl)},
// 	// 						Timeout:   2 * time.Second,
// 	// 					}
// 	// 					_res, err := myClient.Get(link)
// 	// 					if err != nil {
// 	// 						log.Print("FAIL", "%s failed for proxy %s [ERR::%s]", link, proxy, err)
// 	// 					} else {
// 	// 						resLock.Lock()
// 	// 						res = _res
// 	// 						resLock.Unlock()
// 	// 						cancel()
// 	// 					}
// 	// 				}
// 	// 			}
// 	// 		}

// 	// 	}()
// 	// }

// 	// wg.Wait()
// 	// log.Print("SUCCESS", "GET success with proxy %s", link)

// 	// return res, nil
// }
