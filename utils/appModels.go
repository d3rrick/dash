package dash

import (
	"bytes"
	"compress/gzip"
	"crypto/tls"
	"fmt"
	"github.com/hashicorp/go-retryablehttp"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	_ "github.com/tidwall/gjson"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
)

type AppConfig struct {
	Brokers string
	Topic   string
	Proxy   string
	NoProxy string
}

// Scenario struct
type Scenario struct {
	ID            string
	Scenario      string
	Replicas      int
	Examples      string
	ExamplesData  interface{}
	Severity      string
	Priority      string
	Delay         int
	Tag           string
	Service       string
	Status        int
	Headers       map[string]string
	Url           string
	Params        map[string]string
	Method        string
	Auth          Auth
	Type 		  string
	Body          string
	FinalBody     string
	Project       string
	Environment   string
	ExecutionTime string
	Collection    string
	Domain        string
	Developer     string
	Tester        string
	Validators    []struct {
		Validate Validate
	}
	ErrorOutcome    *ErrorOutcome
	ValidateOutcome *ValidateOutcome
	Response        *Response
	RunID           string
	MaskedFields    map[string]string
}

type ReportTemplate struct {
	Scenario              string  `json:"scenario"`
	ID             		string`json:"scenario_id"`
	Tag                   string  `json:"tag"`
	Service               string  `json:"service"`
	Status                int     `json:"status"`
	Severity              string  `json:"severity"`
	Priority              string  `json:"priority"`
	Url                   string  `json:"url"`
	Method                string  `json:"method"`
	Body                  string  `json:"request_body"`
	Headers               string  `json:"headers"`
	Project               string  `json:"project"`
	Domain                string  `json:"domain"`
	Environment           string  `json:"environment"`
	Collection            string  `json:"collection"`
	Validators            string  `json:"validators"`
	RunID                 string  `json:"run_id"`
	ExecutionTime         string  `json:"execution_time"`
	ErrorDescription      string  `json:"error_description"`
	ResponseCode          int     `json:"response_code"`
	ResponseBody          string  `json:"response_body"`
	ResponseTime          float64 `json:"response_time"`
	PassCount             int     `json:"total_pass"`
	FailedCount           int     `json:"total_fail"`
	ValidationDescription string  `json:"validation_description"`
	FinalTestStatus       string  `json:"outcome"`
	Developer             string  `json:"developer"`
	Tester                string  `json:"tester"`
}

// Validate struct
type Validate struct {
	Extract    string
	Comparator string
	Expected   string
}

// Auth struct
type Auth struct {
	Type   string
	Values string
}

// Services struct
type Services struct {
	Name      string
	Auth      Auth
	Method    string
	Tag       string
	Type      string
	Headers   map[string]string
	Developer string
	Tester    string
}

// Config struct
type Config struct {
	Services     []Services
	Data         map[string]string
	Headers      map[string]string
	Metadata     Metadata
	MaskedFields map[string]string
	InitFunc InitFunc
}


type Metadata struct {
	Project     string
	Environment string
	Collection  string
	Domain      string
	Stream bool
}

// ErrorType struct
type ErrorType interface {
	Error() string
}

type Report struct {
	Outcome string
}

type ErrorOutcome struct {
	Reason    string
	ErrorDesc string
}

type ValidateOutcome struct {
	Passed      int
	Failed      int
	FinalStatus string
	Actual      string
}

type Response struct {
	Status int
	Body   string
	Time   float64
}

type InitFunc struct {
	Active bool
	Action string
	Alias string
	GetValue string
	TargetValue string
	URL string
	Headers map[string]string
	Method string
}


var (
	appConfig AppConfig
	defaultTransport *http.Transport
	proxyTransport *http.Transport
	client *http.Client
	request    *http.Request
	err        ErrorType
	bodyBuffer *bytes.Buffer
	res        Response
	reader     io.Reader
	digitCheck = regexp.MustCompile(`^[0-9]+$`)
)

func init(){
	rootDir := RootDir()
	viper.SetConfigType("yaml")
	viper.SetConfigFile(rootDir+"/configs.yaml")
	log.Info("Test tool Location - ",rootDir)

	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); ok {
			log.Fatal("Unable to read the app config file.")
		} else {
			log.Fatalf("\"Unable to read the app config file. Error=%v", err)
		}
	}
	err := viper.Unmarshal(&appConfig)
	if err != nil {
		log.Fatalf("Unable to decode the contents of config file, Error= %v",err)
	}

	defaultTransport = &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}

	if appConfig.Proxy != ""{
		proxyURL, err := url.Parse(appConfig.Proxy)
		if err != nil{
			log.Error(err)
		}
		http.ProxyURL(proxyURL)
		innerTransport := &http.Transport{Proxy: http.ProxyURL(proxyURL),
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
		proxyTransport=innerTransport
	}
	c := retryablehttp.NewClient()
	c.RetryMax = 2
	client = c.StandardClient()
}


func (scenario *Scenario) Request() {
	if scenario.Method != "" {
		scenario.Method = strings.ToUpper(scenario.Method)
	}
	serviceURL, err := url.Parse(scenario.Url)
	if err != nil {
		errorReporter(err, scenario)
		return
	}
	status := digitCheck.MatchString(serviceURL.Hostname())
	if status == true {
		digitCheck.MatchString(serviceURL.Hostname()[1:])
	}
	if stringInSlice(serviceURL.Hostname(), strings.Split(appConfig.NoProxy, ",")) == true {
		client.Transport=defaultTransport
	} else if appConfig.Proxy != ""{
		client.Transport=proxyTransport
	}else{
		client.Transport=defaultTransport
	}
	reqUrl, err := url.Parse(scenario.Url)
	if err != nil {
		errorReporter(err, scenario)
		return
	}
	if len(scenario.Params) != 0 {
		params := url.Values{}
		for k, v := range scenario.Params {
			params.Add(k, v)
			reqUrl.RawQuery = params.Encode()
		}
	}
	if len(scenario.Body) != 0 {
		if scenario.Type =="soap" {
			payload := strings.NewReader(scenario.Body)
			scenario.FinalBody = scenario.Body
			request, err = http.NewRequest(scenario.Method, reqUrl.String(), payload)
		}else {
			bodyBuffer = bytes.NewBuffer([]byte(strings.ReplaceAll(fmt.Sprint(scenario.Body), "\\", ``)))
			scenario.FinalBody = fmt.Sprint(bodyBuffer)
			request, err = http.NewRequest(scenario.Method, reqUrl.String(), bodyBuffer)
		}
	} else {
		request, err = http.NewRequest(scenario.Method, reqUrl.String(), nil)
	}
	if err != nil {
		errorReporter(err, scenario)
		return
	}
	if len(scenario.Headers) != 0 {
		for k, v := range scenario.Headers {
			request.Header[k] = []string{v}
		}
	} else {
		request.Header["Content-Type"] = []string{"application/json"}
	}
	start := time.Now()
	if scenario.Delay != 0{
		time.Sleep(time.Duration(scenario.Delay)*time.Second)
	}
	response, err := client.Do(request)
	stop := time.Since(start)
	//MaskHeaders(scenario)
	if err != nil {
		errorReporter(err, scenario)
		return
	}
	if response != nil {
		resHeader := response.Header.Get("Content-Encoding")
		if resHeader == "gzip" {
			reader, err = gzip.NewReader(response.Body)
			if err != nil {
				errorReporter(err, scenario)
				return
			}
		} else {
			reader = response.Body
		}
		defer response.Body.Close()
	}
	body, err := ioutil.ReadAll(reader)
	if err != nil {
		errorReporter(err, scenario)
		return
	}
	res.Body = string(body)
	res.Status = response.StatusCode
	res.Time = stop.Seconds()
	scenario.Response = &res
	validator(scenario, response, string(body))
}
func (scenario *Scenario) UrlEncodedRequest() {
	var (
		request *http.Request
		err     ErrorType
		res     Response
		reader  io.Reader
	)
	client.Transport=defaultTransport
	if scenario.Method != "" {
		scenario.Method = strings.ToUpper(scenario.Method)
	}
	reqUrl, err := url.Parse(scenario.Url)
	if err != nil {
		errorReporter(err, scenario)
		return
	}
	if len(scenario.Params) != 0 {
		params := url.Values{}
		for k, v := range scenario.Params {
			params.Add(k, v)
			reqUrl.RawQuery = params.Encode()
		}
	}
	if len(scenario.Body) != 0 {
		request, err = http.NewRequest(scenario.Method, reqUrl.String(), strings.NewReader(fmt.Sprint(scenario.Body)))
	} else {
		request, err = http.NewRequest(scenario.Method, reqUrl.String(), nil)
	}
	if err != nil {
		errorReporter(err, scenario)
		return
	}
	if len(scenario.Headers) != 0 {
		for k, v := range scenario.Headers {
			request.Header[k] = []string{v}
		}
	} else {
		request.Header.Add("Content-Type", "application/json")
	}
	start := time.Now()
	if scenario.Delay != 0{
		time.Sleep(time.Duration(scenario.Delay)*time.Second)
	}
	response, err := client.Do(request)
	stop := time.Since(start)

	if err != nil {
		errorReporter(err, scenario)
		return
	}
	if response != nil {
		resHeader := response.Header.Get("Content-Encoding")
		if resHeader == "gzip" {
			reader, err = gzip.NewReader(response.Body)
			if err != nil {
				errorReporter(err, scenario)
				return
			}
		} else {
			reader = response.Body
		}
		defer response.Body.Close()
	}
	body, err := ioutil.ReadAll(reader)
	if err != nil {
		errorReporter(err, scenario)
		return
	}
	if body == nil {
		res.Body = "null"
	} else {
		res.Body = string(body)

	}

	res.Status = response.StatusCode
	res.Time = stop.Seconds()
	scenario.Response = &res
	validator(scenario, response, string(body))
}


func errorReporter(err error, scenario *Scenario) {
	var errOutcome ErrorOutcome
	errOutcome.ErrorDesc = err.Error()
	errOutcome.Reason = "Error parsing response body"
	scenario.ErrorOutcome = &errOutcome
}

func stripSpaces(str string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, str)
}

func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}
