package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sfn"
	"github.com/aws/aws-sdk-go-v2/service/sfn/types"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	SFN_MOCK_CONFIG    = "/home/StepFunctionsLocal/MockConfigFile.json"
	MOUNT_TARGET       = "/home/StepFunctionsLocal/MockConfigFile.json"
	HOST_PATH          = "/MockConfigFile.json"
	STEP_FUNCTION_PATH = "/drain.json"
)

func checkError(err error) {
	if nil != err {
		log.Panicln(err)
	}
}

func TestMain(m *testing.M) {
	m.Run()
}

// A reference implementation written in Java
// https://github.com/aws-samples/aws-stepfunctions-examples/blob/main/sam/demo-local-testing-using-java/src/test/java/com/example/sfn/StepFunctionsLocalJUnitTest.java
func TestDrainResource(t *testing.T) {
	ctx := context.Background()
	cwd, err := os.Getwd()
	checkError(err)

	configMount := testcontainers.ContainerMount{Source: testcontainers.GenericBindMountSource{HostPath: cwd + HOST_PATH}, Target: testcontainers.ContainerMountTarget(MOUNT_TARGET), ReadOnly: true}

	req := testcontainers.ContainerRequest{
		Image:        "amazon/aws-stepfunctions-local",
		ExposedPorts: []string{"8083"},
		Env:          map[string]string{"SFN_MOCK_CONFIG": SFN_MOCK_CONFIG},
		Mounts:       testcontainers.ContainerMounts{configMount},
		WaitingFor:   wait.ForLog("Starting server on port 8083"),
	}
	sfnCont, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})

	if err != nil {
		t.Error(err)
	}

	defer func() {
		if err := sfnCont.Terminate(ctx); err != nil {
			t.Fatalf("failed to terminate container: %s", err.Error())
		}
	}()

	host, err := sfnCont.Host(ctx)
	if err != nil {
		t.Fatalf("Error in getting host of container: %s", err.Error())
	}

	port, err := sfnCont.MappedPort(ctx, "8083/tcp")
	if err != nil {
		t.Fatalf("Error in getting mapped port of container: %s", err.Error())
	}

	// step function options
	// https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/sfn#Options
	localSfnRuntime := fmt.Sprintf("http://%s:%d", host, port.Int())
	log.Println("local sfn runtime: ", localSfnRuntime)
	options := sfn.Options{BaseEndpoint: &localSfnRuntime, Region: "us-east-1"}

	// Create a StepFunctionClient client with additional configuration
	// https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/sfn#New
	sfnClient := sfn.New(options)

	// State Machine Input
	// https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/sfn#CreateStateMachineInput
	definition, err := os.ReadFile(cwd + STEP_FUNCTION_PATH)
	checkError(err)
	stepFunctionDefinition := string(definition)

	name := "DrainTest"
	arn := "arn:aws:iam::123456789012:role/service-role/" + name
	smInput := sfn.CreateStateMachineInput{Definition: &stepFunctionDefinition, Name: &name, RoleArn: &arn}

	// time.Sleep(2*time.Second)

	// Create state machine
	// https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/sfn#Client.CreateStateMachine
	sm, err := sfnClient.CreateStateMachine(ctx, &smInput)

	if nil != err {
		t.Fatalf("Error creating state machine: %s", err.Error())
	}

	log.Println("state machine arn: ", *sm.StateMachineArn)

	executeStepFunction := func(executionName *string, mockToUse *string, executionInput *string) *sfn.GetExecutionHistoryOutput {
		stateMachineArn := fmt.Sprintf("%s#%s", *sm.StateMachineArn, *mockToUse)
		// Execution Options
		// https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/sfn#StartExecutionInput
		executionOptions := sfn.StartExecutionInput{StateMachineArn: &stateMachineArn, Input: executionInput, Name: executionName}

		// Start Execution
		// https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/sfn#Client.StartExecution
		sfnExecution, err := sfnClient.StartExecution(ctx, &executionOptions)

		if nil != err {
			t.Fatalf("Error starting an execution: %s", err.Error())
		}

		log.Println("Execution: ", *sfnExecution.ExecutionArn)

		// https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/sfn#DescribeExecutionInput
		describeExecutionInput := sfn.DescribeExecutionInput{ExecutionArn: sfnExecution.ExecutionArn}
		// Describe Execution
		// https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/sfn#Client.DescribeExecution
		describeExecutionOutput, err := sfnClient.DescribeExecution(ctx, &describeExecutionInput)
		if nil != err {
			t.Fatalf("Error getting execution description: %s", err.Error())
		}

		status := describeExecutionOutput.Status

		// wait for execution to complete
		for types.ExecutionStatusRunning == status {
			time.Sleep(time.Millisecond)
			describeExecutionOutput, err = sfnClient.DescribeExecution(ctx, &describeExecutionInput)
			if nil != err {
				t.Fatalf("Error getting execution description: %s", err.Error())
			}
			status = describeExecutionOutput.Status
		}

		if types.ExecutionStatusSucceeded != status {
			log.Printf("Execution %s ended with %s", *sfnExecution.ExecutionArn, status)
		}

		// Get execution history
		// https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/sfn#GetExecutionHistoryInput
		// https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/sfn#Client.GetExecutionHistory
		executionHistoryOutput, err := sfnClient.GetExecutionHistory(ctx, &sfn.GetExecutionHistoryInput{ExecutionArn: sfnExecution.ExecutionArn, ReverseOrder: true})
		if nil != err {
			t.Fatalf("Error getting execution history: %s", err.Error())
		}

		// Display execution output
		// https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/sfn#GetExecutionHistoryOutput
		for _, ev := range executionHistoryOutput.Events {
			log.Println(ev.Id, " th event ", ev.Type)

			if types.HistoryEventTypeExecutionSucceeded == ev.Type {
				log.Println("Execution succeeded with output :", *ev.ExecutionSucceededEventDetails.Output)
			}
		}

		return executionHistoryOutput
	}

	t.Run("Test a resource that is already drained", func(t *testing.T) {
		// t.Parallel()
		executionName := "AlreadyDrainedResource"
		input := "{\"resourceId\": \"i-00000000\", \"remainingSessions\" : { \"target\" : 0}}"
		executionHistoryOutput := executeStepFunction(&executionName, &executionName, &input)
		expectExecutionSucceedWithRemainingSessions(t, executionHistoryOutput, 0)
	})

	t.Run("Test a resource that is draining", func(t *testing.T) {
		// t.Parallel()
		executionName := "DrainingResource"
		input := "{\"resourceId\": \"i-00000000\", \"waitInterval\": 0, \"drainTime\": {\"min\": \"2000-01-01T00:00:00.000000+00:00\", \"max\" : \"2100-01-01T00:00:00.000000+00:00\"}, \"remainingSessions\" : { \"target\" : 0}}"
		executionHistoryOutput := executeStepFunction(&executionName, &executionName, &input)
		expectExecutionSucceedWithRemainingSessions(t, executionHistoryOutput, 0)
	})

	t.Run("Test a resource that is stucked in draining", func(t *testing.T) {
		// t.Parallel()
		executionName := "StuckedInDrainingResource"
		input := "{\"resourceId\": \"i-00000000\", \"waitInterval\": 0, \"drainTime\": {\"min\": \"2000-01-01T00:00:00.000000+00:00\", \"max\" : \"2100-01-01T00:00:00.000000+00:00\"}, \"remainingSessions\" : { \"target\" : 0}}"
		executionHistoryOutput := executeStepFunction(&executionName, &executionName, &input)
		expectExecutionSucceedWithRemainingSessions(t, executionHistoryOutput, 400)
	})

	t.Run("Test a resource that is slowly draining and hits max allowed time", func(t *testing.T) {
		// t.Parallel()
		executionName := "SlowlyDrainingResource"
		input := "{\"resourceId\": \"i-00000000\", \"waitInterval\": 0, \"drainTime\": {\"min\": \"2000-01-01T00:00:00.000000+00:00\", \"max\" : \"2000-01-01T00:00:00.000000+00:00\"}, \"remainingSessions\" : { \"target\" : 0}}"
		executionHistoryOutput := executeStepFunction(&executionName, &executionName, &input)
		expectExecutionSucceedWithRemainingSessions(t, executionHistoryOutput, 400)
	})

	t.Run("Test that resource is allways allowed minimum time", func(t *testing.T) {
		// t.Parallel()
		executionName := "DrainingResourceMinTime"
		mockToUse := "StuckedInDrainingResource"
		input := "{\"resourceId\": \"i-00000000\", \"waitInterval\": 0, \"drainTime\": {\"min\": \"2100-01-01T00:00:00.000000+00:00\", \"max\" : \"2100-01-01T00:00:00.000000+00:00\"}, \"remainingSessions\" : { \"target\" : 0}}"
		executionHistoryOutput := executeStepFunction(&executionName, &mockToUse, &input)
		expectExecutionSucceedWithRemainingSessions(t, executionHistoryOutput, 0)
	})

	t.Run("Test lamba function invoke generates supported exception", func(t *testing.T) {
		// t.Parallel()
		executionName := "SupportedExceptionInLambda"
		input := "{\"resourceId\": \"i-00000000\", \"remainingSessions\" : { \"target\" : 0}}"
		executionHistoryOutput := executeStepFunction(&executionName, &executionName, &input)
		expectExecutionSucceedWithRemainingSessions(t, executionHistoryOutput, 0)
	})
}

func expectExecutionSucceedWithRemainingSessions(t *testing.T, executionHistoryOutput *sfn.GetExecutionHistoryOutput, expectedRemainingSessions int) {
	if 0 == len(executionHistoryOutput.Events) {
		t.Fatalf("Step function execution resulted zero execution history, this is unexpected")
	}

	lastEvent := executionHistoryOutput.Events[0]

	if types.HistoryEventTypeExecutionFailed == lastEvent.Type {
		t.Fatalf("Step function execution was not successful: %s %s", *lastEvent.ExecutionFailedEventDetails.Cause, *lastEvent.ExecutionFailedEventDetails.Error)
	}

	// get the last successful output to map to check the remaining sessions
	var lastStateOutput map[string]interface{}

	if err := json.Unmarshal([]byte(*lastEvent.ExecutionSucceededEventDetails.Output), &lastStateOutput); nil != err {
		t.Fatalf("Step function execution last event has not valid json output")
	}

	if nil != lastStateOutput && nil != lastStateOutput["lambdaResult"] && nil != lastStateOutput["lambdaResult"].(map[string]interface{}) && nil != (lastStateOutput["lambdaResult"].(map[string]interface{}))["sessions"] {
		remainingSessions := int((lastStateOutput["lambdaResult"].(map[string]interface{}))["sessions"].(float64))
		if remainingSessions != expectedRemainingSessions {
			time.Sleep(60 * time.Second)
			t.Fatalf("Remaining sessions %d is different from expected %d", remainingSessions, expectedRemainingSessions)
		}
	} else {
		t.Fatalf("Step function execution last event output has no valid lambdaResult.sessions")
	}
}
