package stutil

import (
	"encoding/json"
	"errors"
	// "fmt"
	"io"
	"log"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"

	"golang.org/x/net/html"
)

var (
	JClient            http.Client
	RequestObj         ReqObj
	Job                string
	recentLoginAttempt bool = false
	config             Config
)

type ReqObj struct {
	Parameter []map[string]string `json:"parameter"`
}

type Config struct {
	J_username string
	J_password string
	J_address  string
}

func init() {
	jar, err := cookiejar.New(nil)
	if err != nil {
		log.Fatal(err.Error())
	}
	JClient = http.Client{
		Jar: jar,
	}
	conf, err := os.Open("conf.json")
	if err != nil {
		panic(errors.New("Unable to open conf!"))
	}
	decoder := json.NewDecoder(conf)
	err = decoder.Decode(&config)
	if err != nil {
		log.Println("Bad decode")
	}
}

func LogInToJenkins() (err error) {
	defer func() {
		if r := recover(); r != nil {
			var ok bool
			err, ok = r.(error)
			if !ok {
				log.Println("Recover went bad")
				err = errors.New("Recover went bad")
			}
		}
	}()
	values := &url.Values{"j_username": {config.J_username}, "j_password": {config.J_password}, "remember_me": {"on"}, "from": {"/"}, "json": {"{\"j_username\": " + config.J_username + ", \"j_password\": " + config.J_password + ", \"remember_me\": true, \"from\": \"/\"}"}, "Submit": {"log in"}}
	resp, err := JClient.PostForm(config.J_address+"/j_acegi_security_check", *values)
	if err != nil {
		log.Println("Error")
		log.Printf("%v\n", err.Error())
		return
	}
	defer resp.Body.Close()
	url, err := url.Parse(config.J_address)
	if err != nil {
		log.Println("ERROR from url.Parse: " + err.Error())
		return
	}
	cookies := JClient.Jar.Cookies(url)
	for i := range cookies {
		log.Println(cookies[i].String())
	}
	return nil
}

func postToJob(urlValues url.Values, address string) error {
	log.Println("POSTING TO JOB")
	log.Println(address)
	log.Println(urlValues)
	resp, err := JClient.PostForm(address, urlValues)
	defer resp.Body.Close()
	if err != nil {
		return err
	}
	if resp.StatusCode == 404 {
		if recentLoginAttempt {
			log.Println("Job not found")
			return errors.New("Job Not Found")
		}
		log.Println("Got 404, trying something else")
		LogInToJenkins()
		recentLoginAttempt = true
		err = postToJob(urlValues, address)
		if err != nil {
			return err
		}
	} else if resp.StatusCode == 500 {
		return errors.New("Post to job failed (500)")
	}
	recentLoginAttempt = false
	return nil
}

func parseJobConfig(r io.Reader) (url.Values, error) {
	requestObj, err := getReqObj(r)
	if err != nil {
		log.Println(err.Error())
		return nil, err
	}
	jsonValues, err := json.Marshal(requestObj)
	if err != nil {
		log.Println(err.Error())
		return nil, err
	}
	urlValues := buildConfigValuesFromJson(jsonValues)
	return urlValues, nil
}

func buildConfigValuesFromJson(jsonValues []byte) url.Values {
	configValues := url.Values{}
	configValues.Add("statusCode", "303")
	configValues.Add("redirectTo", ".")
	configValues.Add("Submit", "Build")
	configValues.Add("json", string(jsonValues))
	for _, pair := range RequestObj.Parameter {
		configValues.Add(pair["name"], pair["value"])
	}
	return configValues
}

func getReqObj(r io.Reader) (*ReqObj, error) {
	RequestObj := &ReqObj{}
	d := html.NewTokenizer(r)

	for {
		tokenType := d.Next()
		if tokenType == html.ErrorToken {
			log.Println("Done parsing html")
			return RequestObj, nil
		}

		if tokenType == html.StartTagToken {
			var jsonBit = make(map[string]string)
			name, attr := d.TagName()
			//First level: element is a div that has attributes
			if string(name) == "div" && attr {
				attrKey, attrValue, _ := d.TagAttr()
				//make sure we're only dealing with divs that have "name"="parameter"
				//Second Level: Only Divs that have "name"="parameter" attribute
				if string(attrKey) == "name" && string(attrValue) == "parameter" {
					//first `input` is the setting name; second is the value
					for i := 0; i < 2; i++ {
						tokenType = d.Next()
						//After the first input, there is a blank TextToken that must be
						//skipped
						if tokenType == html.TextToken {
							tokenType = d.Next()
						}
						name, attr = d.TagName()
						//if input, is either the setting name or the setting input (both
						//are input elements)
						switch string(name) {
						case "input":
							for attrKey, attrValue, _ := d.TagAttr(); attrKey != nil; attrKey, attrValue, _ = d.TagAttr() {
								//only care about attrs named "value"
								if string(attrKey) == "value" {
									//first `input`; the setting name
									if i == 0 {
										// configValues.Add("name", string(attrValue))
										jsonBit["name"] = string(attrValue)
										//second `input`; the setting value
									} else {
										name, attr = d.TagName()
										// configValues.Add("value", string(attrValue))
										jsonBit["value"] = string(attrValue)
										RequestObj.Parameter = append(RequestObj.Parameter, jsonBit)
									}
								}
							}
							//in case of select, i is expected to be 1
						case "select":
							tokenType = d.Next()
							for name, attr = d.TagName(); string(name) == "option"; tokenType = d.Next() {
								name, attr = d.TagName()
								for _, attrValue, attrMore := d.TagAttr(); attrMore; _, attrValue, attrMore = d.TagAttr() {
									jsonBit["value"] = string(attrValue)
									RequestObj.Parameter = append(RequestObj.Parameter, jsonBit)
								}
							}
						default:
							if i == 1 {
								return RequestObj, errors.New("Unsupported input type: " + string(name))
							}
						}
					}
				}
			}
		}
	}
}

func buildAddress(jobName string) string {
	return config.J_address + "/job/" + jobName + "/build?delay=0sec"
}

func rebuildAddress(jobName string) string {
	return config.J_address + "/job/" + jobName + "/lastCompletedBuild/rebuild/configSubmit"
}

func parseStringToJson(config string) ([]byte, error) {
	RequestObj := ReqObj{}
	var pairPieces []string
	var jsonBit map[string]string
	pairs := strings.Split(config, ", ")
	for _, pair := range pairs {
		jsonBit = make(map[string]string)
		pair = strings.Trim(pair, "{}")
		pair = strings.Replace(pair, "\"", "", -1)
		pairPieces = strings.Split(pair, ":")
		for index, piece := range pairPieces {
			pairPieces[index] = strings.Trim(piece, " ")
		}
		if len(pairPieces) < 2 {
			log.Println("\nERROR")
			log.Println("Failed to parse config: ", config)
			return []byte(""), errors.New("Error parsing supplied config")
		}
		jsonBit["name"] = pairPieces[0]
		jsonBit["value"] = pairPieces[1]
		RequestObj.Parameter = append(RequestObj.Parameter, jsonBit)
	}
	jsonValues, err := json.Marshal(RequestObj)
	if err != nil {
		log.Println(err.Error())
		log.Println("\nRETURNING ERROR")
		return []byte(""), err
	}
	return jsonValues, nil
}

func BuildJob(jobName string, config string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			var ok bool
			err, ok = r.(error)
			if !ok {
				err = errors.New("Error not recovered well")
			}
		}
	}()
	address := buildAddress(jobName)
	resp, err := JClient.Get(address)
	if err != nil {
		log.Println("Bad GET: %v\n", err)
		return err
	}
	defer resp.Body.Close()

	urlValues := url.Values{}
	if config == "" {
		urlValues, err = parseJobConfig(resp.Body)
		if err != nil {
			return err
		}
	} else {
		jsonConfig, err := parseStringToJson(config)
		if err != nil {
			log.Println("ERROR IN BUILD JOB WITH CONFIG")
			log.Println(err.Error())
			return err
		}
		urlValues = buildConfigValuesFromJson(jsonConfig)
	}

	err = postToJob(urlValues, address)
	if err != nil {
		return err
	}

	return nil
}

func RebuildJob(jobName string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			var ok bool
			err, ok = r.(error)
			if !ok {
				err = errors.New("Error not recovered well")
			}
		}
	}()
	address := rebuildAddress(config.J_address + "/job/" + jobName + "/lastCompletedBuild/rebuild/parameterized")
	resp, err := JClient.Get(address)
	if err != nil {
		log.Println(err.Error())
		return err
	}
	defer resp.Body.Close()

	urlValues, err := parseJobConfig(resp.Body)
	if err != nil {
		log.Println(err.Error())
		return err
	}
	err = postToJob(urlValues, address)
	if err != nil {
		return err
	}

	return nil
}

func GetJobConfig(jobName string) (config string, err error) {
	defer func() {
		if r := recover(); r != nil {
			var ok bool
			err, ok = r.(error)
			if !ok {
				err = errors.New("Error not recovered well")
			}
		}
	}()
	configAddress := buildAddress(jobName)
	resp, err := JClient.Get(configAddress)
	if err != nil {
		log.Println(err.Error())
		return "", err
	} else if resp.StatusCode == 404 {
		if recentLoginAttempt {
			log.Println("Job not found")
			return "", errors.New("Job Not Found")
		}
		log.Println("404 Received; relogging and trying again")
		LogInToJenkins()
		recentLoginAttempt = true
		config, err = GetJobConfig(jobName)
		if err != nil {
			return "", err
		}
		return config, nil
	} else if resp.StatusCode == 500 {
		return "", errors.New("Post to job failed (500)")
	}
	recentLoginAttempt = false
	obj, err := getReqObj(resp.Body)
	if err != nil {
		log.Println(err.Error())
		return "", err
	}
	if len(obj.Parameter) == 0 {
		log.Println(obj)
		return "", errors.New("Job is unparameterized")
	} else {
		config = "{"
		for _, pair := range obj.Parameter {
			config += "\"" + pair["name"] + "\" : \"" + pair["value"] + "\", "
		}
		log.Println(config)
		config = config[:len(config)-2]
		config += "}"
		return config, nil
	}

}
