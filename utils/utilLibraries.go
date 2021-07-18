package app

import (
	"encoding/csv"
	"encoding/json"
	xj "github.com/basgys/goxml2json"
	"github.com/olekukonko/tablewriter"
	log "github.com/sirupsen/logrus"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"

	"fmt"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Knetic/govaluate"
	"github.com/rs/xid"
	_ "github.com/satori/go.uuid"
	uuid "github.com/satori/go.uuid"
	"github.com/segmentio/kafka-go"
	"github.com/tidwall/gjson"
)

var (
	mux                sync.Mutex
	regex              = regexp.MustCompile("{{(.*?)}}")
	kafkaScenarioQueue []kafka.Message
)

func Worker(scenario Scenario, config Config, finalScenarioChan chan Scenario) {
	mux.Lock()
	getService(&scenario, config)
	bodyConfigs(&scenario, config)
	validatorConfigs(&scenario, config)
	urlConfigs(&scenario, config)
	switch scenario.Tag {
	case "plain":
		scenario.Request()
		finalScenarioChan <- scenario
	case "urlencoded":
		scenario.UrlEncodedRequest()
		finalScenarioChan <- scenario
	default:
		scenario.Request()
		finalScenarioChan <- scenario
	}
	mux.Unlock()

}
func getService(scenario *Scenario, config Config) {
	if scenario.Method != "" {
		scenario.Method = strings.ToUpper(scenario.Method)
	}
	for _, i := range config.Services {
		if i.Name == scenario.Service {
			scenario.Project = config.Metadata.Project
			scenario.Environment = config.Metadata.Environment
			scenario.Collection = config.Metadata.Collection
			scenario.Domain = config.Metadata.Domain
			scenario.Developer = i.Developer
			scenario.Tester = i.Tester
			scenario.Tag = i.Tag
			scenario.Type = i.Type
			scenario.Auth = i.Auth

			if i.Headers != nil && scenario.Headers != nil {
				for k, v := range i.Headers {
					scenario.Headers[k] = v
				}
			} else if i.Headers != nil && scenario.Headers == nil {
				scenario.Headers = i.Headers
			}
		}
	}

	if scenario.Headers == nil && config.Headers != nil {
		scenario.Headers = config.Headers
	} else if config.Headers != nil && scenario.Headers != nil {
		for k, v := range config.Headers {
			scenario.Headers[k] = v
		}
	}
	for k, v := range scenario.Headers {
		found := regex.FindAllString(v, -1)
		if len(found) != 0 {
			gotten := found[0]
			replaced := trimmer(gotten)
			if replaced == "uuid" {
				guid := xid.New()
				scenario.Headers[k] = guid.String()
			}
			if replaced == "guid" {
				u := uuid.NewV4()
				scenario.Headers[k] = u.String()
			}
			if replaced == "timestamp" {
				scenario.Headers[k] = string(time.Now().Unix())
			}
			if config.Data[replaced] != "" {
				scenario.Headers[k] = config.Data[replaced]
			}
		}
	}
}
func bodyConfigs(scenario *Scenario, config Config) {
	scenario.MaskedFields = config.MaskedFields
	found := regex.FindAllString(fmt.Sprint(scenario.Body), -1)
	if len(found) != 0 {
		scenario.Body = recurse(found, scenario.Body, config)

	}
}
func validatorConfigs(scenario *Scenario, config Config) {
	for i, v := range scenario.Validators {
		found := regex.FindAllString(v.Validate.Expected, -1)
		if len(found) == 0 {
			continue
		}
		gotten := found[0]
		replaced := trimmer(gotten)
		cfgData := templateVariables(replaced, config)
		scenario.Validators[i].Validate.Expected = strings.ReplaceAll(v.Validate.Expected, gotten, cfgData)
	}
}
func urlConfigs(scenario *Scenario, config Config) {
	found := regex.FindAllString(scenario.Url, -1)
	if len(found) != 0 {
		for len := range found {
			gotten := found[len]
			replaced := trimmer(gotten)
			cfgData := templateVariables(replaced, config)
			out := strings.ReplaceAll(scenario.Url, gotten, cfgData)
			scenario.Url = out
		}
	}
	if scenario.Params != nil {
		for k, v := range scenario.Params {
			found := regex.FindAllString(v, -1)
			if len(found) == 0 {
				continue
			}
			gotten := found[0]
			replaced := trimmer(gotten)
			cfgData := templateVariables(replaced, config)
			out := strings.ReplaceAll(v, gotten, cfgData)
			scenario.Params[k] = out
		}
	}
}
func recurse(found []string, str string, config Config) string {
	i := len(found) - 1
	if i < 0 {
		return str
	} else {
		work, found := found[i], found[:i]
		i = i - 1
		replaced := trimmer(work)
		cfgData := templateVariables(replaced, config)
		str = strings.ReplaceAll(str, work, cfgData)
		return recurse(found, str, config)
	}
}
func validator(scenario *Scenario, response *http.Response, body string) {
	var validateOutcome ValidateOutcome
	var testReport Report

	if scenario.Type == "soap" {
		xml := strings.NewReader(body)
		jsonData, err := xj.Convert(xml)
		if err != nil {
			log.Info("could not convert the response to json")
		}
		if jsonData != nil {
			body = jsonData.String()
		}
		log.Info("could not convert the response to json")
	}

	//var errReport ErrorReport
	statusValidation := fmt.Sprintf("%d%s%d", scenario.Status, "==", response.StatusCode)
	expression, _ := govaluate.NewEvaluableExpression(statusValidation)
	result, _ := expression.Evaluate(nil)
	if result == true {
		validateOutcome.Passed += 1
		validateOutcome.Actual += fmt.Sprintln("Passed -- Expected ", statusValidation)
	} else {
		validateOutcome.Failed += 1
		validateOutcome.Actual += fmt.Sprintln("Failed  -- Expected ", statusValidation)
	}
	for _, v := range scenario.Validators {
		extract := gjson.Get(body, v.Validate.Extract)
		comp := v.Validate.Comparator
		expected := v.Validate.Expected
		_statusValidation := fmt.Sprintf("'%s' %s '%s'", extract, comp, expected)
		exp, err := govaluate.NewEvaluableExpression(_statusValidation)
		if err != nil {
			//errReport.ErrorDesc = err.Error()
			//errReport.Reason = "Invalid comparator"
			//scenario.ErrorReport = &errReport
			continue
		}
		result, err := exp.Evaluate(nil)
		if err != nil {
			//errReport.ErrorDesc = err.Error()
			//errReport.Reason = "Error performing comparison"
			//scenario.ErrorReport = &errReport
			//
			continue
		}

		if result == true {
			validateOutcome.Passed += 1
			validateOutcome.Actual += fmt.Sprintln("Passed -- Expected ", _statusValidation)
		} else {
			validateOutcome.Failed += 1
			validateOutcome.Actual += fmt.Sprintln("Failed -- Expected ", _statusValidation)
		}
	}
	if validateOutcome.Failed > 0 {
		validateOutcome.FinalStatus = "failed"
		testReport.Outcome = "failed"
	} else {
		validateOutcome.FinalStatus = "passed"
		testReport.Outcome = "passed"
	}
	scenario.ValidateOutcome = &validateOutcome
}
func templateVariables(rep string, config Config) string {
	cfg := config.Data[rep]
	if rep == "guid" {
		guid := xid.New()
		rep = guid.String()
		return rep
	} else if rep == "timestamp" {
		return fmt.Sprint(time.Now().Unix())
	} else if rep == "uuid" {
		u := uuid.NewV4()
		return u.String()
	} else if cfg != "" {
		return cfg
	}
	return fmt.Sprintf("{{%s}}", rep)
}
func trimmer(str string) string {
	return strings.TrimLeft(strings.TrimRight(str, "}}"), "{{")
}
func MaskHeaders(scenario *Scenario) {
	for k, _ := range scenario.Headers {
		for ik, iv := range scenario.MaskedFields {
			if k == ik {
				scenario.Headers[k] = iv
			}
		}
	}
}
func GetFinalReport(scenario Scenario) ReportTemplate {
	var reportTemplate ReportTemplate
	jsonHeaders, _ := json.Marshal(scenario.Headers)
	if scenario.ErrorOutcome != nil {
		reportTemplate.ErrorDescription = scenario.ErrorOutcome.ErrorDesc
	}
	if scenario.ValidateOutcome != nil {
		reportTemplate.PassCount = scenario.ValidateOutcome.Passed
		reportTemplate.FailedCount = scenario.ValidateOutcome.Failed
		reportTemplate.ValidationDescription = scenario.ValidateOutcome.Actual
		reportTemplate.FinalTestStatus = scenario.ValidateOutcome.FinalStatus
	} else {
		reportTemplate.FinalTestStatus = "error"
	}
	if scenario.Response != nil {
		reportTemplate.ResponseBody = strings.Replace(scenario.Response.Body, "\"", "'", -1)
		reportTemplate.ResponseCode = scenario.Response.Status
		reportTemplate.ResponseTime = scenario.Response.Time
	} else if scenario.Response == nil {
		reportTemplate.ResponseBody = "null"
		reportTemplate.ResponseCode = 0
		reportTemplate.ResponseTime = 0
	}

	reportTemplate.Scenario = scenario.Scenario
	reportTemplate.Service = scenario.Service
	reportTemplate.Tag = scenario.Tag
	reportTemplate.Status = scenario.Status
	reportTemplate.Url = scenario.Url
	reportTemplate.ID = scenario.ID
	reportTemplate.Method = scenario.Method
	reportTemplate.Severity = scenario.Severity
	reportTemplate.Priority = scenario.Priority
	reportTemplate.Body = strings.Replace(scenario.Body, "\"", "'", -1)
	reportTemplate.Headers = strings.Replace(string(jsonHeaders), "\"", "'", -1)
	reportTemplate.Project = scenario.Project
	reportTemplate.Environment = scenario.Environment
	reportTemplate.Collection = scenario.Collection
	reportTemplate.Validators = fmt.Sprint(scenario.Validators)
	reportTemplate.RunID = scenario.RunID
	reportTemplate.ExecutionTime = scenario.ExecutionTime
	reportTemplate.Developer = scenario.Developer
	reportTemplate.Tester = scenario.Tester
	reportTemplate.Domain = scenario.Domain
	scenarioStream, _ := json.Marshal(&scenario)
	kafkaScenarioQueue = append(kafkaScenarioQueue, kafka.Message{Value: scenarioStream})
	return reportTemplate
}
func RootDir() string {
	/**
	Build environment,

	When you wat to create binaries for various environments,
	uncomment this section,
	folderPath, err := osext.ExecutableFolder()
	if err != nil {
		log.Fatal(err)
	}
	return  folderPath

	**/

	/**
	Testing env
	When executing source code use the snippet below.

	**/
	_, b, _, _ := runtime.Caller(0)
	d := path.Join(path.Dir(b))
	return filepath.Dir(d)

}
func Commander(totalScenarios int, finalScenarios chan Scenario, sessionID string, reportOut *string, verboseMsg *string, scenarios []Scenario, config Config) {
	printerChan := make(chan ReportTemplate, totalScenarios)
	csvChan := make(chan ReportTemplate, totalScenarios)

	var reports []ReportTemplate
	for i := 0; i < totalScenarios; i++ {
		go Worker(scenarios[i], config, finalScenarios)
	}
	for a := 1; a <= totalScenarios; a++ {
		scenario := <-finalScenarios
		scenario.RunID = sessionID
		scenario.ExecutionTime = time.Now().Format("2006-01-02 15:04:05")
		reportTemplate := GetFinalReport(scenario)
		reports = append(reports, reportTemplate)
		printerChan <- reportTemplate
		csvChan <- reportTemplate
		reportStream, _ := json.Marshal(&reportTemplate)
		kafkaScenarioQueue = append(kafkaScenarioQueue, kafka.Message{Value: reportStream})
		if *verboseMsg != "" {
			out, _ := json.Marshal(&reportTemplate)
			fmt.Println(string(out))
		}
	}
	close(csvChan)
	close(printerChan)
	close(finalScenarios)

	switch *reportOut {
	case "csv":
		SaveToCSV(csvChan)
	case "json":
		saveToJson(reports, sessionID)
	case "all":
		SaveToCSV(csvChan)
		saveToJson(reports, sessionID)
	}

	var data [][]string
	for pc := range printerChan {
		dt := []string{pc.Service, pc.Scenario, pc.Url, pc.FinalTestStatus, strconv.Itoa(pc.PassCount), strconv.Itoa(pc.FailedCount)}
		data = append(data, dt)
	}
	table := tablewriter.NewWriter(os.Stdout)
	table.SetHeader([]string{"Service", "Scenario", "URl", "Final Status", "Passed No", "Failed No."})
	table.SetBorders(tablewriter.Border{Left: true, Top: false, Right: true, Bottom: false})
	table.SetCenterSeparator("|")
	table.AppendBulk(data)
	table.Render()
}

func SaveToCSV(scenarioChan chan ReportTemplate) {
	///GENERATE CSV FILE REPORT
	file, err := os.Create("testresult_" + time.Now().Format("2006_01_02_15_04_05") + ".csv")
	defer file.Close()
	if err != nil {
		log.Error("Cannot create file", err)
	}

	writer := csv.NewWriter(file)
	defer writer.Flush()
	var data2 [][]string
	csvheader := []string{"SERVICE", "SCENARIO", "FINAL STATUS", "PASSED NO.", "FAILED NO.", "REQUEST BODY", "RESPONSE BODY", "VALIDATION DESCRIPTION", "URL"}
	data2 = append(data2, csvheader)
	var data3 [][]string
	for pc1 := range scenarioChan {
		csvdata := []string{pc1.Service, pc1.Scenario, pc1.FinalTestStatus, strconv.Itoa(pc1.PassCount), strconv.Itoa(pc1.FailedCount), pc1.Body, pc1.ResponseBody, pc1.ValidationDescription, pc1.Url}
		data3 = append(data3, csvdata)
	}
	for _, csvhder := range data2 {
		err = writer.Write(csvhder)
		for _, value := range data3 {
			err = writer.Write(value)
			if err != nil {
				log.Error("Cannot write to file", err)
			}
		}
	}
}

func saveToJson(reports []ReportTemplate, sessionID string) {
	file, err := json.MarshalIndent(reports, "", " ")
	if err != nil {
		log.Error(err)
	}
	err = ioutil.WriteFile(fmt.Sprintf("Report-%s.json", strings.Replace(sessionID, ":", "-", -1)), file, 0644)
	if err != nil {
		log.Error(err)
	}
}
