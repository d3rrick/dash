
package main

import (
	"flag"
	"fmt"
	"github.com/derrick-gopher/dash/cmd"
	app "github.com/derrick-gopher/dash/utils"
	"github.com/kyokomi/emoji/v2"
	uuid "github.com/satori/go.uuid"
	log "github.com/sirupsen/logrus"
	"os"
	"time"
)

var (
	configsPath  *string
	scenarioPath *string
	scenarios    []app.Scenario
	config app.Config
	sessionID string
	ReportOutput *string
	verboseMsg *string
	runAt time.Time
)

func init() {
	_, _ = emoji.Println(":hugging: DASH v.1.0.0 :hugging:")
	configsPath = flag.String("c", "", "config file")
	scenarioPath = flag.String("s", "", "scenarios directory/file")
	ReportOutput = flag.String("o", "", "report output format, supported json, csv, all")
	verboseMsg = flag.String("v", "", "show a detailed log before writing to other formats")
	flag.Parse()

	if *configsPath == "" || *scenarioPath == "" {
		flag.PrintDefaults()
		os.Exit(1)
	}
	if *ReportOutput == ""{
		log.Info("No output format passed, therefore ignored.")
	}
	config = cmd.GetConfigs(configsPath)
	scenarios = cmd.GetScenarios(scenarioPath)

	u := uuid.NewV4()
	runAt = time.Now()
	sessionID = runAt.Format("2006-01-02T15:04:05")+"_"+ u.String()
}

func main() {
	totalScenarios := len(scenarios)
	finalScenarios := make(chan app.Scenario, totalScenarios)
	log.Info("Running Tests!")
	_, _ = emoji.Println(":gear::gear::gear: Running Tests! :gear::gear::gear:")
	fmt.Println("Test outcome >>> see results.json // results.csv for a detailed report. >>>")
	app.Commander(totalScenarios,finalScenarios,sessionID,ReportOutput,verboseMsg,scenarios, config)
	_, _ = emoji.Println("Testing completed!! :hourglass:")
	fmt.Printf("Run %d in %s ", totalScenarios,time.Since(runAt) )
}

