package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"

	"github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi"
	rgtTypes "github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi/types"

	"github.com/google/uuid"
)

// sample implementation
// https://aws.amazon.com/blogs/networking-and-content-delivery/automating-connectivity-assessments-with-vpc-reachability-analyzer/
const (
	requestTimeout = 8 * time.Minute
)

// Lambda events
// https://docs.aws.amazon.com/lambda/latest/dg/golang-handler.html
// https://pkg.go.dev/encoding/json#Unmarshal
// https://docs.aws.amazon.com/cli/latest/reference/lambda/invoke.html
// https://pkg.go.dev/github.com/aws/aws-lambda-go/events
func main() {
	lambda.Start(handler)
	/*
		tags := Tags{}

		err := json.Unmarshal([]byte(`
			{
				"tags": [
				  {
					"key": "Name",
					"value": "AudioGwReachabilityTest"
				  },
				  {
					"key": "Env",
					"value": "Canary"
				  }
				]
			  }
		`), &tags)
		if err != nil {
			log.Println("error:", err)
		}
		handler(tags)
	*/
}

type Tag struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type Tags struct {
	Tags []Tag `json:"tags"`
}

type Config struct {
	allowEmptyTags          bool
	maxReachabilityAnalysis int
}

func (c Config) String() string {
	return fmt.Sprintf("{allowEmptyTags: %t, maxReachabilityAnalysis: %d}", c.allowEmptyTags, c.maxReachabilityAnalysis)
}

const (
	DEFAULT_ALLOW_EMPTY_TAGS          = false
	ENV_ALLOW_EMPTY_TAGS              = "ALLOW_EMPTY_TAGS"
	DEFAULT_MAX_REACHABILITY_ANALYSIS = 10
	ENV_MAX_REACHABILITY_ANALYSIS     = "MAX_REACHABILITY_ANALYSIS"
)

// Use the following priority
// arg > env > default
func getConfig() Config {
	allowEmptyTags := bool(DEFAULT_ALLOW_EMPTY_TAGS)
	isAllowedInStr, ok := os.LookupEnv(ENV_ALLOW_EMPTY_TAGS)
	if ok {
		if "" == isAllowedInStr {
			allowEmptyTags = true
		} else {
			isAllowed, err := strconv.ParseBool(isAllowedInStr)
			if err == nil {
				allowEmptyTags = isAllowed
			}
		}
	}

	maxReachabilityAnalysis := int(DEFAULT_MAX_REACHABILITY_ANALYSIS)
	maxReachabilityAnalysisInStr, ok := os.LookupEnv(ENV_MAX_REACHABILITY_ANALYSIS)
	if ok {
		maxValue, err := strconv.ParseInt(maxReachabilityAnalysisInStr, 10, 0 /*bitsize for int*/)
		if err == nil {
			maxReachabilityAnalysis = int(maxValue)
		}
	}

	allowEmptyTagsPtr := flag.Bool("allowEmptyTags", allowEmptyTags, "allow empty tags, effectuvely trigger all reachability analysis available")
	maxReachabilityAnalysisPtr := flag.Int("maxAnalysis", maxReachabilityAnalysis, "maximum numer of triggered reachability alanysis")

	flag.Parse()

	return Config{allowEmptyTags: *allowEmptyTagsPtr, maxReachabilityAnalysis: *maxReachabilityAnalysisPtr}
}

func handler(tags Tags) (ResultEvent, error) {

	progConfig := getConfig()
	log.Println("Using config: ", progConfig)

	// avoid using empty tags if it is not specially declared to be that way.
	// this will be in order to avoid selecting all resources without any tag filter
	if 0 == len(tags.Tags) && !progConfig.allowEmptyTags {
		log.Println("Empty tags and configuration is not set accordingly.")
		log.Println("Quiting processing any further to avoid triggereing all resource analysis")
		return ResultEvent{}, errors.New("empty tags without configuration set accordingly")
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	cfg, err := config.LoadDefaultConfig(ctx)
	if nil != err {
		log.Println("Error getting default config", err)
		return ResultEvent{}, err
	}

	analysisPaths, err := getPaths(ctx, cfg, tags)
	if nil != err {
		log.Println("Error getting paths", err)
		return ResultEvent{}, err
	}

	if len(analysisPaths) > progConfig.maxReachabilityAnalysis {
		log.Printf("Limiting paths to analyse. Will get first %d out of %d", progConfig.maxReachabilityAnalysis, len(analysisPaths))
		analysisPaths = analysisPaths[:progConfig.maxReachabilityAnalysis]
	}

	log.Println("Paths to be anlysed")
	for _, v := range analysisPaths {
		log.Println(v)
	}

	analysisResult := doAnalysis(ctx, cfg, analysisPaths)
	log.Println("Analysis Results")
	for _, ar := range analysisResult {
		log.Println(ar)
	}

	return ResultEvent{Results: analysisResult}, nil
}

func getPaths(ctx context.Context, cfg aws.Config, tags Tags) ([]string, error) {
	filters := []rgtTypes.TagFilter{}
	for _, v := range tags.Tags {
		filters = append(filters, rgtTypes.TagFilter{Key: &v.Key, Values: []string{v.Value}})
	}

	// Create a ResourceGroupsTaggingAPI client with additional configuration
	// https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi
	rga := resourcegroupstaggingapi.NewFromConfig(cfg)

	// https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi#Client.GetResources
	// https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi#GetResourcesInput
	resources, err := rga.GetResources(ctx, &resourcegroupstaggingapi.GetResourcesInput{ResourceTypeFilters: []string{"ec2:network-insights-path"}, TagFilters: filters})

	if nil != err {
		return nil, err
	}

	// https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/resourcegroupstaggingapi#GetResourcesOutput
	if nil == resources || 0 == len(resources.ResourceTagMappingList) {
		log.Println("There are no paths with corresponding tags")
		return []string{}, nil
	}

	// Compile the expression once, usually at init time.
	// Any character except : or / from the end
	idSeperator := regexp.MustCompile(`[^:/]*$`)

	analysisPaths := make([]string, 0)
	for _, v := range resources.ResourceTagMappingList {
		analysisPaths = append(analysisPaths, idSeperator.FindString(*(v.ResourceARN)))
	}

	return analysisPaths, nil
}

type AnalysisResult struct {
	Path        string               `json:"path"`
	Arn         string               `json:"arn"`
	Status      types.AnalysisStatus `json:"status"`
	IsReachable bool                 `json:"isReachable"`
}

type ResultEvent struct {
	Results []AnalysisResult `json:"results"`
}

func (ar AnalysisResult) String() string {
	return fmt.Sprintf("{path: %s, arn: %s, status: %s, reachable: %t}", ar.Path, ar.Arn, ar.Status, ar.IsReachable)
}

func doAnalysis(ctx context.Context, cfg aws.Config, analysisPaths []string) []AnalysisResult {
	awsEc2 := ec2.NewFromConfig(cfg)
	var wg sync.WaitGroup

	analysisResults := make([]AnalysisResult, len(analysisPaths))

	for idx, path := range analysisPaths {
		analysisResults[idx].Path = path
		analyze(ctx, &wg, awsEc2, &(analysisResults[idx]))
	}

	wg.Wait()

	return analysisResults
}

func analyze(ctx context.Context, wg *sync.WaitGroup, awsEc2 *ec2.Client, analysisResult *AnalysisResult) {
	analysisResult.Arn = "nil"
	analysisResult.Status = types.AnalysisStatusFailed
	analysisResult.IsReachable = false
	(*wg).Add(1)
	go func() {
		defer (*wg).Done()

		clientToken := uuid.New().String()

		prefix := fmt.Sprintf("[%s]", analysisResult.Path)
		logger := log.New(os.Stdout, prefix, log.LstdFlags)
		logger.SetPrefix(prefix)

		// https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/ec2#Client.StartNetworkInsightsAnalysis
		startNiaResult, err := awsEc2.StartNetworkInsightsAnalysis(ctx, &ec2.StartNetworkInsightsAnalysisInput{ClientToken: &clientToken, NetworkInsightsPathId: &analysisResult.Path})

		if nil != err {
			logger.Println("Error in starting analysis ", analysisResult.Path, err)
			return
		}

		status := types.AnalysisStatusRunning

		// https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/ec2#StartNetworkInsightsAnalysisOutput
		// https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/ec2@v1.110.1/types#NetworkInsightsAnalysis

		if nil != startNiaResult && nil != startNiaResult.NetworkInsightsAnalysis {
			if nil != startNiaResult.NetworkInsightsAnalysis.NetworkInsightsAnalysisArn {
				analysisResult.Arn = *startNiaResult.NetworkInsightsAnalysis.NetworkInsightsAnalysisArn
				logger.Println("Analysis Arn: ", analysisResult.Arn)
			}

			status = startNiaResult.NetworkInsightsAnalysis.Status
		}

		analysisId := *startNiaResult.NetworkInsightsAnalysis.NetworkInsightsAnalysisId
		niaIds := []string{analysisId}

		var niaResult *ec2.DescribeNetworkInsightsAnalysesOutput

		for types.AnalysisStatusRunning == status {
			time.Sleep(10 * time.Second)
			// https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/ec2#Client.DescribeNetworkInsightsAnalyses
			// https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/ec2#DescribeNetworkInsightsAnalysesInput
			niaResult, err = awsEc2.DescribeNetworkInsightsAnalyses(ctx, &ec2.DescribeNetworkInsightsAnalysesInput{NetworkInsightsAnalysisIds: niaIds})

			if nil != err {
				logger.Println("Error in getting analysis status ", err)
				return
			}

			// https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/ec2#DescribeNetworkInsightsAnalysesOutput
			if nil != niaResult && 0 != len(niaResult.NetworkInsightsAnalyses) {
				status = niaResult.NetworkInsightsAnalyses[0].Status
				logger.Println(analysisId, " : ", niaResult.NetworkInsightsAnalyses[0].Status)
			}
		}
		analysisResult.Status = status

		if nil != niaResult && 0 != len(niaResult.NetworkInsightsAnalyses) && nil != niaResult.NetworkInsightsAnalyses[0].NetworkPathFound {
			analysisResult.IsReachable = *(niaResult.NetworkInsightsAnalyses[0].NetworkPathFound)
		}
	}()
}
