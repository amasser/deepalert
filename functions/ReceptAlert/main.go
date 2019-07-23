package main

import (
	"context"
	"encoding/json"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"

	"github.com/m-mizutani/deepalert"
	f "github.com/m-mizutani/deepalert/functions"
)

type lambdaArguments struct {
	Event                 events.SNSEvent
	InspecterDelayMachine string
	ReviewerDelayMachine  string
	CacheTable            string
	Region                string
}

func mainHandler(args lambdaArguments) error {
	svc := f.NewDataStoreService(args.CacheTable, args.Region)

	for _, msg := range f.SNStoMessages(args.Event) {
		var alert deepalert.Alert
		if err := json.Unmarshal(msg, &alert); err != nil {
			return errors.Wrap(err, "Fail to unmarshal alert from AlertNotification")
		}

		reportID, isNew, err := svc.TakeReportID(alert)
		if err != nil {
			return errors.Wrapf(err, "Fail to take reportID for alert: %v", alert)
		}
		f.SetLoggerReportID(reportID)

		f.Logger.WithFields(logrus.Fields{
			"ReportID": reportID,
			"isNew":    isNew,
			"Error":    err,
			"AlertID":  alert.AlertID(),
		}).Info("ReportID has been retrieved")

		report := deepalert.Report{
			ID:     reportID,
			Alerts: []deepalert.Alert{alert},
			Status: deepalert.StatusNew,
		}

		if err := svc.SaveAlertCache(reportID, alert); err != nil {
			return errors.Wrap(err, "Fail to save alert cache")
		}

		if err := f.ExecDelayMachine(args.InspecterDelayMachine, args.Region, &report); err != nil {
			return errors.Wrap(err, "Fail to execute InspectorDelayMachine")
		}

		if isNew {
			if err := f.ExecDelayMachine(args.ReviewerDelayMachine, args.Region, &report); err != nil {
				return errors.Wrap(err, "Fail to execute ReviewerDelayMachine")
			}
		}
	}

	return nil
}

func handleRequest(ctx context.Context, event events.SNSEvent) error {
	f.SetLoggerContext(ctx, deepalert.NullReportID)
	f.Logger.WithField("event", event).Info("Start")

	args := lambdaArguments{
		Event:                 event,
		InspecterDelayMachine: os.Getenv("DISPATCH_MACHINE"),
		ReviewerDelayMachine:  os.Getenv("REVIEW_MACHINE"),
		CacheTable:            os.Getenv("CACHE_TABLE"),
		Region:                os.Getenv("AWS_REGION"),
	}

	if err := mainHandler(args); err != nil {
		f.Logger.WithError(err).Error("Fail")
		return err
	}

	return nil
}

func main() {
	lambda.Start(handleRequest)
}
