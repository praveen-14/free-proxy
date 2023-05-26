package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/nfx/go-htmltable"
)

type Proxy struct {
	IP    string
	Port  string
	Https bool
}

func unique(proxies []Proxy) []Proxy {
	keys := make(map[string]Proxy)
	for _, p := range proxies {
		keys[fmt.Sprintf("%s-%s-%t", p.IP, p.Port, p.Https)] = p
	}
	v := make([]Proxy, 0, len(keys))
	for _, value := range keys {
		v = append(v, value)
	}
	return v
}

func is_up(proxy Proxy) bool {
	timeout := 5 * time.Second
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(proxy.IP, proxy.Port), timeout)
	if err != nil {
		return false
	}
	if conn != nil {
		defer conn.Close()
		return true
	}
	return false
}

func (server *ProxyServer) StartProxySearch(ctx context.Context) {
	server.GetProxiesAPIHttps(true, ctx)
	server.GetProxiesAPIHttps(false, ctx)
	server.GetProxiesUsProxies(true, ctx)
	server.GetProxiesUsProxies(false, ctx)
	// GetSpysProxies(true, ctx, proxyChan, workingProxyChan)
	// GetSpysProxies(false, ctx, proxyChan, workingProxyChan)

	// go func() {
	// 	for {
	// 		proxies := []Proxy{}

	// 		proxies = append(proxies, GetProxiesAPIHttps(true, ctx, proxyChan, workingProxyChan)...)
	// 		proxies = append(proxies, GetProxiesAPIHttps(false, proxyChan, workingProxyChan)...)

	// 		proxies = append(proxies, GetProxiesUsProxies(true, proxyChan, workingProxyChan)...)
	// 		proxies = append(proxies, GetProxiesUsProxies(false, proxyChan, workingProxyChan)...)

	// 		proxies = unique(proxies)
	// 		rand.Seed(time.Now().UnixNano())
	// 		rand.Shuffle(len(proxies), func(i, j int) { proxies[i], proxies[j] = proxies[j], proxies[i] })

	// 		for _, p := range proxies {
	// 			select {
	// 			case <-ctx.Done():
	// 				return
	// 			default:
	// 				proxyChan <- p
	// 			}
	// 		}
	// 	}

	// }()
}

func StartProxyFiltering(ctx context.Context, searchChan chan Proxy) chan Proxy {

	filterChan := make(chan Proxy, 100)
	for i := 0; i < 100; i++ {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case proxy := <-searchChan:
					if is_up(proxy) {
						filterChan <- proxy
					}
				}
			}
		}()
	}

	return filterChan
}

func (server *ProxyServer) GetProxiesAPIHttps(https bool, ctx context.Context) {
	go func() {

		for {
			select {
			case <-ctx.Done():
				return
			default:

				url := "https://api.proxyscrape.com/v2/?request=displayproxies&protocol=http&timeout=10000&country=all&ssl=all&anonymity=all"
				if https {
					url = "https://api.proxyscrape.com/v2/?request=displayproxies&protocol=https&timeout=10000&country=all&ssl=all&anonymity=all"
				}
				res, err := server.Get(url, server.Config.ProxySearchThreadLimit, server.Config.ProxySearcNumMaxRetries, func(r *http.Response, err error) (bool, error) { return false, nil }, true)
				if err != nil {
					continue
				}
				_responseData, _ := ioutil.ReadAll(res.Body)
				res.Body.Close()
				responseData := string(_responseData)

				for _, addr := range strings.Split(responseData, "\n") {
					if len(addr) > 0 {
						server.proxyChan <- Proxy{
							IP:    strings.TrimSpace(strings.Split(addr, ":")[0]),
							Port:  strings.TrimSpace(strings.Split(addr, ":")[1]),
							Https: true,
						}
					}

				}
			}
		}
	}()
}

func (server *ProxyServer) GetProxiesUsProxies(https bool, ctx context.Context) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:

				type _Proxy struct {
					IP    string `header:"IP Address"`
					Port  string `header:"Port"`
					Https string `header:"Https"`
				}

				htmltable.Logger = func(_ context.Context, msg string, fields ...any) {
					// disabling logs
				}

				url := "https://www.us-proxy.org/"
				_proxies, _ := htmltable.NewSliceFromURL[_Proxy](url)

				for _, p := range _proxies {
					val := Proxy{
						IP:    strings.TrimSpace(p.IP),
						Port:  strings.TrimSpace(p.Port),
						Https: false,
					}
					if p.Https == "yes" {
						val.Https = true
					}
					if https == val.Https {
						server.proxyChan <- val
					}
				}
			}
		}
	}()
}

// func GetSpysProxies(https bool, ctx context.Context, proxyChan chan Proxy, workingProxyChan chan Proxy) {
// 	countries := []string{"NL", "SG", "US", "DE", "ID", "BR", "CO", "FR", "RU", "GB", "IN", "BD", "VN", "EC", "HK", "TR", "ES", "JP", "ZA", "AR", "IR", "PK", "VE", "MX", "TW", "TH", "CN", "KR", "PE", "EG", "CL", "UA", "DO", "PL", "IT", "CA", "KE", "YE", "KH", "MY", "AU", "FI", "CZ", "PH", "SE", "HU", "NP", "NG", "IQ", "LY", "BG", "HN", "KZ", "RS", "PS", "RO", "IE", "GT", "PY", "SA", "AL", "GR", "LB", "BY", "SK", "MD", "CH", "LV", "TZ", "AT", "UZ", "BA", "AM", "GE", "AF", "BO", "AE", "LT", "HR", "PR", "IL", "NO", "MN", "CR", "MW", "KG", "MM", "MG", "PA", "CM", "MK", "GH", "UG", "XK", "UY", "PT", "NI", "QA", "MZ", "BE", "NZ", "SY", "CD", "ZW", "CY", "LK", "MV", "GN", "SI", "AZ", "BW", "NE", "DK", "ZM", "EE", "SV", "LA", "BJ", "AO", "BI", "SC", "TJ", "JO", "LS", "KW", "TN", "MU", "MT", "TL", "LU", "ME", "CU", "HT", "GQ", "RW", "OM", "CI", "DZ", "MA", "ML", "MO", "CG", "AD", "SO", "PG", "TD", "SL", "LR", "SS", "IS", "SN", "JM", "BH", "KY", "NA", "WS", "SD", "BS", "BT", "ET", "FJ", "VG", "GA", "SZ", "BZ", "GY", "CF", "TT", "YT", "CV", "NC", "GI", "SX", "AG", "DJ", "GM", "ER", "BB", "SR", "BM", "TM", "GU", "LI", "BF"}
// 	go func() {
// 		for {
// 			select {
// 			case <-ctx.Done():
// 				return
// 			default:

// 				for _, c := range countries {
// 					link := fmt.Sprintf("https://spys.one/proxys/%s/", c)
// 					res, err := Get(link, proxyChan, workingProxyChan, Proxy{}, CONCURRENCY, MAX_RETRIES, func(r *http.Response, err error) bool { return false })
// 					if err != nil {
// 						continue
// 					}

// 					doc, _ := goquery.NewDocumentFromResponse(res)
// 					res.Body.Close()

// 					ips := doc.Find("tr.spy1xx")
// 					ips.Each(func(i int, s *goquery.Selection) {
// 						ip := strings.Split(s.Find("font.spy14").First().Text(), "document")[0]
// 						port := s.Find("font.spy2").First().Text()
// 						fmt.Println(ip, port)
// 					})
// 					os.Exit(0)

// 				}

// 			}
// 		}
// 	}()
// }

// req.Header.Set(`authority`, `spys.one`)
// req.Header.Set(`method`, `GET`)
// req.Header.Set(`path`, `/proxys/NL/`)
// req.Header.Set(`scheme`, `https`)
// req.Header.Set(`accept`, `text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.9`)
// req.Header.Set(`accept-encoding`, `gzip, deflate, br`)
// req.Header.Set(`accept-language`, `en-US,en;q=0.9`)
// req.Header.Set(`sec-ch-ua`, `"Chromium";v="107", "Not=A?Brand";v="24"`)
// req.Header.Set(`sec-ch-ua-mobile`, `?0`)
// req.Header.Set(`sec-ch-ua-platform`, `"Linux"`)
// req.Header.Set(`sec-fetch-dest`, `document`)
// req.Header.Set(`sec-fetch-mode`, `navigate`)
// req.Header.Set(`sec-fetch-site`, `none`)
// req.Header.Set(`sec-fetch-user`, `?1`)
// req.Header.Set(`upgrade-insecure-requests`, `1`)
// req.Header.Set(`user-agent`, `Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/107.0.0.0 Safari/537.36`)

// func GetUSProxies(ctx context.Context) (<-chan Proxy, error) {

// 	type Proxy struct {
// 		IP    string `header:"IP Address"`
// 		Port  string `header:"Port"`
// 		Https string `header:"Https"`
// 	}

// 	htmltable.Logger = func(_ context.Context, msg string, fields ...any) {
// 		// disabling logs
// 	}

// 	proxyChan := make(chan Proxy, 100)
// 	url := "https://www.us-proxy.org/"

// 	go func() {
// 		defer close(proxyChan)

// 		for {
// 			select {
// 			case <-ctx.Done():
// 				return
// 			default:
// 				proxies, _ := htmltable.NewSliceFromURL[Proxy](url)
// 				for _, proxy := range proxies {
// 					// if proxy.Https == "yes" {
// 					proxyChan <- proxy
// 					// }
// 				}
// 			}
// 		}

// 	}()

// 	return proxyChan, nil
// }

// type USProxy struct {
// 	IP   string `header:"IP address"`
// 	Port string `header:"Port"`
// }

// func GetUSProxies(ctx context.Context) (<-chan USProxy, error) {

// 	htmltable.Logger = func(_ context.Context, msg string, fields ...any) {
// 		// disabling logs
// 	}

// 	proxyChan := make(chan USProxy, 100)
// 	url := "https://vpnoverview.com/privacy/anonymous-browsing/free-proxy-servers/"

// 	go func() {
// 		defer close(proxyChan)

// 		for {
// 			select {
// 			case <-ctx.Done():
// 				return
// 			default:
// 				proxies, _ := htmltable.NewSliceFromURL[USProxy](url)
// 				for _, proxy := range proxies {
// 					proxyChan <- proxy
// 				}
// 			}
// 		}

// 	}()

// 	return proxyChan, nil
// }
