package main

import (
	"bytes"
	"encoding/xml"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// PRTG structure for XML output
type Result struct {
	XMLName xml.Name  `xml:"prtg"`
	Result  []Channel `xml:"result"`
}

type Channel struct {
	XMLName         xml.Name `xml:"result"`
	Channel         string   `xml:"channel"`
	Value           string   `xml:"value"`
	Unit            string   `xml:"unit"`
	CustomUnit      string   `xml:"customunit,omitempty"`
	LimitMode       int      `xml:"limitmode"`
	Float           int      `xml:"float,omitempty"`
	LimitErrorMsg   string   `xml:"limiterrormsg,omitempty"`
	LimitWarningMsg string   `xml:"limitwarningmsg,omitempty"`
	LimitErrorMin   string   `xml:"limitminerror,omitempty"`
	LimitErrorMax   string   `xml:"limitmaxerror,omitempty"`
	LimitWarningMin string   `xml:"limitminwarning,omitempty"`
	LimitWarningMax string   `xml:"limitmaxwarning,omitempty"`
	Warning         string   `xml:"warning,omitempty"`
	ValueLookup     string   `xml:"valuelookup,omitempty"`
}

// Structure to hold file counts by date
type ChannelData struct {
	Date  string
	Count int
}

// Structure to hold channel information
type ChannelCategory struct {
	Name    string
	Records map[string]int // date to file count mapping
}

// Function to parse the file name
func parseFileName(fileName string) (channel string, date string, err error) {
	// Example fileName: azadi-01-Jul-24-02:34:07.audio.m4a

	// Remove the file extension
	nameParts := strings.Split(fileName, ".")
	if len(nameParts) < 2 {
		return "", "", fmt.Errorf("invalid file format: %s", fileName)
	}

	// Split the name part by hyphen to extract the channel name and date-time
	parts := strings.Split(nameParts[0], "-")
	if len(parts) < 5 {
		return "", "", fmt.Errorf("invalid file format: %s", fileName)
	}

	channel = parts[0]
	day := parts[1]
	month := parts[2]
	year := parts[3]
	timePart := parts[4]

	// Combine date and time
	dateString := fmt.Sprintf("%s-%s-%s %s", day, month, year, timePart)

	// Parse the date string into a time.Time object
	parsedDate, err := time.Parse("02-Jan-06 15:04:05", dateString)
	if err != nil {
		return "", "", fmt.Errorf("invalid date format in file: %s", fileName)
	}

	date = parsedDate.Format("2006-01-02") // Format date as YYYY-MM-DD
	return channel, date, nil
}

// Function to monitor and categorize files
func monitorFolder(output string) (map[string]*ChannelCategory, error) {
	lines := strings.Split(output, "\n")

	channelMap := make(map[string]*ChannelCategory)

	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) > 8 {
			fileName := fields[8]
			channel, date, err := parseFileName(fileName)
			if err != nil {
				//log.Println("Skipping file:", fileName, "Error:", err)
				continue
			}

			if _, exists := channelMap[channel]; !exists {
				channelMap[channel] = &ChannelCategory{
					Name:    channel,
					Records: make(map[string]int),
				}
			}
			channelMap[channel].Records[date]++
		}
	}
	return channelMap, nil
}

func compareDates(time1 string, t2 time.Time) bool {
	t1, err := time.Parse("2006-01-02", time1)
	if err != nil {
		return false
	}
	return t1.Year() == t2.Year() && t1.Month() == t2.Month() && t1.Day() == t2.Day()
}

func main() {
	var name string
	var hostname string
	var username string
	var key string
	var port string
	var path string
	var channelNames string

	flag.StringVar(&name, "name", "node-name", "Node name. Default is node-name.")
	flag.StringVar(&hostname, "hostname", "", "Hostname. Required.")
	flag.StringVar(&username, "user", "root", "Username. Default is root.")
	flag.StringVar(&key, "key", "", "SSH Private Key. Required.")
	flag.StringVar(&port, "port", "22", "SSH Port. Default is 22.")
	flag.StringVar(&path, "path", "/var/rec", "Device Index. Default is /var/rec.")
	flag.StringVar(&channelNames, "chan", "itn,azadi,voa,pars,bbc,one", "Channels Names Camma seperate. Default is itn,azadi,voa,pars,bbc,one")

	channels := strings.Split(channelNames, ",")

	flag.Parse()

	if hostname == "" || key == "" {
		log.Fatal("Please supply required arguments.")
	}

	keyBytes, err := ioutil.ReadFile(key)
	if err != nil {
		log.Fatalf("Unable to read private key: %v", err)
	}
	signer, err := ssh.ParsePrivateKey(keyBytes)
	if err != nil {
		return
	}
	// SSH connection configuration
	sshConfig := &ssh.ClientConfig{
		User: username,
		Auth: []ssh.AuthMethod{
			ssh.PublicKeys(signer),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}

	// Server address
	serverAddress := fmt.Sprintf("%s:%s", hostname, port)

	// Connect to the server
	conn, err := ssh.Dial("tcp", serverAddress, sshConfig)
	if err != nil {
		//fmt.Printf("Failed to dial: %v\n", err)
		printConnectionFailure()
		return
	}
	defer conn.Close()

	// Create a session
	session, err := conn.NewSession()
	if err != nil {
		//fmt.Printf("Failed to create session: %v\n", err)
		printConnectionFailure()
		return
	}
	defer session.Close()

	// Run the command and capture the output
	command := "ls -lha " + path
	var stdoutBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	if err := session.Run(command); err != nil {
		//fmt.Printf("Failed to run: %v\n", err)
		printConnectionFailure()
		return
	}

	channelMap, err := monitorFolder(stdoutBuf.String())
	if err != nil {
		log.Fatal(err)
	}

	for index := range channels {
		exist := false
		for channel, _ := range channelMap {
			if channels[index] == channel {
				exist = true
			}
		}
		if !exist {
			channelMap[channels[index]] = &ChannelCategory{
				Name:    channels[index],
				Records: make(map[string]int),
			}
		}
	}
	//displayResults(channelMap)

	channelResult := make(map[string]int)
	channelTodayResult := make(map[string]int)
	for channel, data := range channelMap {
		channelTodayResult[channel] = 0
		channelResult[channel] = 0
		for date, count := range data.Records {
			if compareDates(date, time.Now()) {
				channelTodayResult[channel] = count
			}
			channelResult[channel] += count
		}
	}

	prtg := Result{}
	prtg.Result = append(prtg.Result, Channel{
		Channel:     "Connection Health",
		Value:       "0",
		Unit:        "Interger",
		LimitMode:   0,
		ValueLookup: "prtg.customlookups.gvb-sensor.timeout",
		Warning:     "1",
	})

	for channel, count := range channelResult {
		prtg.Result = append(prtg.Result, Channel{
			Channel:         fmt.Sprintf("%s Total files", channel),
			LimitMode:       1,
			LimitErrorMax:   "70",
			LimitWarningMax: "50",
			LimitErrorMsg:   "Too much file are stored",
			LimitWarningMsg: "Transfering files failed",
			Value:           fmt.Sprintf("%d", count),
			Unit:            "custom",
			CustomUnit:      "files",
		})
		prtg.Result = append(prtg.Result, Channel{
			Channel:    fmt.Sprintf("%s Today Rec", channel),
			LimitMode:  0,
			Value:      fmt.Sprintf("%d", channelTodayResult[channel]),
			Unit:       "custom",
			CustomUnit: "files",
		})
	}

	output, err := xml.MarshalIndent(prtg, "", "  ")
	if err != nil {
		fmt.Println("Error generating XML:", err)
		return
	}

	// Print the XML
	fmt.Println(string(output))
}

func printConnectionFailure() {
	prtgResult := Result{
		Result: []Channel{
			{
				Channel:     "Connection Health",
				Value:       "1",
				Unit:        "Interger",
				LimitMode:   0,
				ValueLookup: "prtg.customlookups.gvb-sensor.timeout",
				Warning:     "1",
			},
		},
	}
	output, err := xml.MarshalIndent(prtgResult, "", "  ")
	if err != nil {
		log.Fatalf("Error marshalling XML: %s", err)
	}

	// Print XML
	fmt.Println(xml.Header + string(output))
}
