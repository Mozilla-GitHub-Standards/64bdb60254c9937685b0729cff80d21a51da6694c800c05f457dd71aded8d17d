package main

import (
	"fmt"
	"net"
	"net/url"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
)

const (
	reaper_tag = "REAPER"
	s_sep      = "|"
	s_tformat  = "2006-01-02 03:04PM MST"
)

type Instance struct {
	AWSResource
	LaunchTime      time.Time
	SecurityGroups  map[string]string
	InstanceType    string
	PublicIPAddress net.IP
}

func NewInstance(region string, instance *ec2.Instance) *Instance {
	i := Instance{
		AWSResource: AWSResource{
			Id:     *instance.InstanceID,
			Region: region, // passed in cause not possible to extract out of api
			Tags:   make(map[string]string),
		},

		SecurityGroups: make(map[string]string),
		LaunchTime:     *instance.LaunchTime,
		InstanceType:   *instance.InstanceType,
	}

	for _, sg := range instance.SecurityGroups {
		i.SecurityGroups[*sg.GroupID] = *sg.GroupName
	}

	for _, tag := range instance.Tags {
		i.Tags[*tag.Key] = *tag.Value
	}

	switch *instance.State.Code {
	case 0:
		i.State = PENDING
	case 16:
		i.State = RUNNING
	case 32:
		i.State = SHUTTINGDOWN
	case 48:
		i.State = TERMINATED
	case 64:
		i.State = STOPPING
	case 80:
		i.State = STOPPED
	}

	// TODO: untested
	if instance.PublicIPAddress != nil {
		i.PublicIPAddress = net.ParseIP(*instance.PublicIPAddress)
	}

	i.Name = i.Tag("Name")
	i.ReaperState = ParseState(i.Tags[reaper_tag])

	return &i
}

func (i *Instance) AWSConsoleURL() *url.URL {
	url, err := url.Parse(fmt.Sprintf("https://%s.console.aws.amazon.com/ec2/v2/home?region=%s#Instances:instanceId=%s",
		i.Region, i.Region, i.Id))
	if err != nil {
		Log.Error(fmt.Sprintf("Error generating AWSConsoleURL. %s", err))
	}
	return url
}

// Autoscaled checks if the instance is part of an autoscaling group
func (i *Instance) AutoScaled() (ok bool) { return i.Tagged("aws:autoscaling:groupName") }

func (i *Instance) Filter(filter Filter) bool {
	matched := false
	// map function names to function calls
	switch filter.Function {
	case "Pending":
		if b, err := filter.BoolValue(0); err == nil && i.Pending() == b {
			matched = true
		}
	case "Running":
		if b, err := filter.BoolValue(0); err == nil && i.Running() == b {
			matched = true
		}
	case "ShuttingDown":
		if b, err := filter.BoolValue(0); err == nil && i.ShuttingDown() == b {
			matched = true
		}
	case "Terminated":
		if b, err := filter.BoolValue(0); err == nil && i.Terminated() == b {
			matched = true
		}
	case "Stopping":
		if b, err := filter.BoolValue(0); err == nil && i.Stopping() == b {
			matched = true
		}
	case "Stopped":
		if b, err := filter.BoolValue(0); err == nil && i.Stopped() == b {
			matched = true
		}
	case "InstanceType":
		if i.InstanceType == filter.Arguments[0] {
			matched = true
		}
	case "Tagged":
		if i.Tagged(filter.Arguments[0]) {
			matched = true
		}
	case "Tag":
		if i.Tag(filter.Arguments[0]) == filter.Arguments[1] {
			matched = true
		}
	case "HasPublicIPAddress":
		if i.PublicIPAddress != nil {
			matched = true
		}
	case "PublicIPAddress":
		if i.PublicIPAddress.String() == filter.Arguments[0] {
			matched = true
		}
	// uses RFC3339 format
	// https://www.ietf.org/rfc/rfc3339.txt
	case "LaunchTimeBefore":
		t, err := time.Parse(time.RFC3339, filter.Arguments[0])
		if err == nil && t.After(i.LaunchTime) {
			matched = true
		}
	case "LaunchTimeAfter":
		t, err := time.Parse(time.RFC3339, filter.Arguments[0])
		if err == nil && t.Before(i.LaunchTime) {
			matched = true
		}
	default:
		Log.Error("No function %s could be found for filtering ASGs.", filter.Function)
	}
	return matched
}

func Whitelist(region, instanceId string) error {
	api := ec2.New(&aws.Config{Region: region})
	req := &ec2.CreateTagsInput{
		Resources: []*string{aws.String(instanceId)},
		Tags: []*ec2.Tag{
			&ec2.Tag{
				Key:   aws.String("REAPER_SPARE_ME"),
				Value: aws.String("true"),
			},
		},
	}

	_, err := api.CreateTags(req)

	if err != nil {
		return err
	}

	return nil
}

func (i *Instance) Terminate() (bool, error) {
	api := ec2.New(&aws.Config{Region: i.Region})
	req := &ec2.TerminateInstancesInput{
		InstanceIDs: []*string{aws.String(i.Id)},
	}

	resp, err := api.TerminateInstances(req)

	if err != nil {
		return false, err
	}

	if len(resp.TerminatingInstances) != 1 {
		return false, fmt.Errorf("Instance could %s not be terminated.", i.Id)
	}

	return true, nil
}

func (i *Instance) ForceStop() (bool, error) {
	return i.Stop()
}

func (i *Instance) Stop() (bool, error) {
	api := ec2.New(&aws.Config{Region: i.Region})
	req := &ec2.StopInstancesInput{
		InstanceIDs: []*string{aws.String(i.Id)},
	}

	resp, err := api.StopInstances(req)

	if err != nil {
		return false, err
	}

	if len(resp.StoppingInstances) != 1 {
		return false, fmt.Errorf("Instance %s could not be stopped.", i.Id)
	}

	return true, nil
}
