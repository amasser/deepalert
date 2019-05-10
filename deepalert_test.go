package deepalert_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/guregu/dynamo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/m-mizutani/deepalert"
	"github.com/m-mizutani/deepalert/test"
	gp "github.com/m-mizutani/generalprobe"
)

func TestNormalWorkFlow(t *testing.T) {
	cfg := test.LoadTestConfig(".")
	alertKey := uuid.New().String()

	alert := deepalert.Alert{
		Detector:  "test",
		RuleName:  "TestRule",
		AlertKey:  alertKey,
		Timestamp: time.Now().UTC(),
		Attributes: []deepalert.Attribute{
			{
				Type:    deepalert.TypeIPAddr,
				Key:     "test value",
				Value:   "192.168.0.1",
				Context: []deepalert.AttrContext{deepalert.CtxLocal},
			},
		},
	}
	alertMsg, err := json.Marshal(alert)
	require.NoError(t, err)

	var reportID string

	playbook := []gp.Scene{
		// Send request
		gp.PublishSnsMessage(gp.LogicalID("AlertNotification"), alertMsg),
		gp.GetLambdaLogs(gp.LogicalID("ReceptAlert"), func(log gp.CloudWatchLog) bool {
			assert.Contains(t, log, alertKey)
			return true
		}).Filter(alertKey),
		gp.GetDynamoRecord(gp.LogicalID("CacheTable"), func(table dynamo.Table) bool {
			var entry struct {
				ReportID string `dynamo:"report_id"`
			}

			alertID := "alertmap/" + alert.AlertID()
			err := table.Get("pk", alertID).Range("sk", dynamo.Equal, "Fixed").One(&entry)
			if err != nil {
				return false
			}
			require.NotEmpty(t, entry.ReportID)
			reportID = entry.ReportID
			return true
		}),
		gp.GetLambdaLogs(gp.LogicalID("DispatchInspection"), func(log gp.CloudWatchLog) bool {
			return log.Contains(reportID)
		}),
		gp.GetLambdaLogs(gp.LogicalID("SubmitReport"), func(log gp.CloudWatchLog) bool {
			return log.Contains(reportID)
		}),
		gp.GetLambdaLogs(gp.LogicalID("FeedbackAttribute"), func(log gp.CloudWatchLog) bool {
			return log.Contains(reportID)
		}),
		gp.GetLambdaLogs(gp.LogicalID("FeedbackAttribute"), func(log gp.CloudWatchLog) bool {
			return log.Contains("mizutani")
		}),
		gp.GetDynamoRecord(gp.LogicalID("CacheTable"), func(table dynamo.Table) bool {
			var caches []struct {
				Key   string `dynamo:"attr_key"`
				Value string `dynamo:"attr_value"`
				Type  string `dynamo:"attr_type"`
			}

			pk := "attribute/" + reportID
			if err := table.Get("pk", pk).All(&caches); err != nil {
				return false
			}

			if len(caches) != 2 {
				return false
			}

			var a1, a2 int
			if caches[0].Type == "ipaddr" {
				a1, a2 = 0, 1
			} else {
				a1, a2 = 1, 0
			}

			assert.Equal(t, "192.168.0.1", caches[a1].Value)
			assert.Equal(t, "mizutani", caches[a2].Value)
			assert.Equal(t, "username", caches[a2].Type)
			return true
		}),

		gp.GetDynamoRecord(gp.LogicalID("CacheTable"), func(table dynamo.Table) bool {
			var contents []struct {
				Data []byte `dynamo:"data"`
			}

			pk := "content/" + reportID

			if err := table.Get("pk", pk).All(&contents); err != nil {
				return false
			}

			require.True(t, len(contents) > 0)
			require.NotEmpty(t, contents[0].Data)
			return true
		}),

		gp.Pause(10),

		gp.GetLambdaLogs(gp.LogicalID("CompileReport"), func(log gp.CloudWatchLog) bool {
			return log.Contains(reportID)
		}),
		gp.GetLambdaLogs(gp.LogicalID("PublishReport"), func(log gp.CloudWatchLog) bool {
			return log.Contains(reportID)
		}),
	}

	err = gp.New(cfg.Region, cfg.StackName).Play(playbook)
	require.NoError(t, err)
}