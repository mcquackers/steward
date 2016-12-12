package main

import (
	"encoding/json"

	"flag"
	"html/template"
	"log"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"

	"bitbucket.org/atlassianlabs/hipchat-golang-base/util"

	"github.com/gorilla/mux"
	"github.com/tbruyelle/hipchat-go/hipchat"
	"github.com/user/stewql"
	"github.com/user/stutil"
)

//RoomConfig holds information to send authenticated messages to all rooms
type RoomConfig struct {
	token *hipchat.OAuthAccessToken
	hc    *hipchat.Client
	name  string
}

// Context keeps context of the running application
type Context struct {
	baseURL string
	static  string
	rooms   map[string]*RoomConfig
}

func getRoomID(payLoad map[string]interface{}) string {
	return strconv.Itoa(int((payLoad["item"].(map[string]interface{}))["room"].(map[string]interface{})["id"].(float64)))
}

func getMessage(payLoad map[string]interface{}) string {
	return payLoad["item"].(map[string]interface{})["message"].(map[string]interface{})["message"].(string)
}

func getUserId(payLoad map[string]interface{}) string {
	return payLoad["item"].(map[string]interface{})["message"].(map[string]interface{})["from"].(map[string]interface{})["links"].(map[string]interface{})["self"].(string)[32:]
}

func isAuthUser(payLoad map[string]interface{}) (isAuth bool, err error) {
	userId := getUserId(payLoad)
	log.Println(userId)
	isAuth, err = stewql.IsAuthUser(userId)
	log.Println(isAuth)
	return
}

func (c *Context) healthcheck(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode([]string{"OK"})
}

func (c *Context) atlassianConnect(w http.ResponseWriter, r *http.Request) {
	lp := path.Join("./static", "atlassian-connect.json")
	log.Println(r)
	vals := map[string]string{
		"LocalBaseUrl": c.baseURL,
	}
	tmpl, err := template.ParseFiles(lp)
	if err != nil {
		log.Fatalf("%v", err)
	}
	tmpl.ExecuteTemplate(w, "config", vals)
}

func (c *Context) installable(w http.ResponseWriter, r *http.Request) {
	authPayload, err := util.DecodePostJSON(r, true)
	if err != nil {
		log.Fatalf("Parsed auth data failed: %v\n", err)
	}

	credentials := hipchat.ClientCredentials{
		ClientID:     authPayload["oauthId"].(string),
		ClientSecret: authPayload["oauthSecret"].(string),
	}

	roomName := strconv.Itoa(int(authPayload["roomId"].(float64)))
	newClient := hipchat.NewClient("")
	tok, _, err := newClient.GenerateToken(credentials, []string{hipchat.ScopeSendNotification})
	if err != nil {
		log.Fatalf("Client.GetAccessToken returns an error %v", err)
	}
	rc := &RoomConfig{
		name: roomName,
		hc:   tok.CreateClient(),
	}
	c.rooms[roomName] = rc

	util.PrintDump(w, r, false)
	json.NewEncoder(w).Encode([]string{"OK"})
}

func (c *Context) getUserId(w http.ResponseWriter, r *http.Request) {
	log.Println("Received GETUSERID request")
	payLoad, err := util.DecodePostJSON(r, true)
	if err != nil {
		log.Fatalf("Failed to parse payload: %s\n", err.Error())
		return
	}

	roomID := getRoomID(payLoad)

	userId := getUserId(payLoad)

	c.notifyRoom(roomID, userId, "purple")
	return
}

func (c *Context) addPreset(w http.ResponseWriter, r *http.Request) {
	log.Println("Received ADDPRESET request")
	payLoad, err := util.DecodePostJSON(r, true)
	if err != nil {
		log.Fatalf("Failed to parse payload: %s\n", err.Error())
		return
	}

	roomID := getRoomID(payLoad)

	isAuth, err := isAuthUser(payLoad)
	if err != nil {
		c.notifyRoom(roomID, err.Error(), "red")
	}
	if !isAuth {
		c.notifyRoom(roomID, "You must be an authorized user to do this", "red")
		return
	}

	var entry string = getMessage(payLoad)
	command := strings.SplitN(entry, " ", 4)
	if len(command) != 4 {
		c.notifyRoom(roomID, "Preset command must follow syntax: <!addPreset> <jobName> <presetName> <configJson>", "red")
		return
	}

	err = stewql.AddPreset(command[1], command[2], command[3])
	if err != nil {
		c.notifyRoom(roomID, err.Error(), "red")
		return
	}
	_, err = stewql.ListPreset(command[1], command[2])
	if err != nil {
		c.notifyRoom(roomID, "Preset was not successfully added, but AddPreset did not throw an error", "yellow")
		return
	}
	c.notifyRoom(roomID, "Preset Successfully Added", "green")
}

func (c *Context) listPreset(w http.ResponseWriter, r *http.Request) {
	log.Println("Received LISTPRESET request")
	payLoad, err := util.DecodePostJSON(r, true)
	if err != nil {
		log.Fatalf("Failed to parse payload: " + err.Error())
		return
	}
	roomID := getRoomID(payLoad)

	isAuth, err := isAuthUser(payLoad)
	if err != nil {
		c.notifyRoom(roomID, err.Error(), "red")
	}
	if !isAuth {
		c.notifyRoom(roomID, "You must be an authorized user to do this", "red")
		return
	}

	var entry string = getMessage(payLoad)
	command := strings.SplitN(entry, " ", 3)
	if len(command) < 3 || strings.Contains(command[2], " ") {
		c.notifyRoom(roomID, "listPreset command must follow syntax: <!listPreset> <jobName> <presetName>", "red")
		return
	}

	preset, err := stewql.ListPreset(command[1], command[2])
	if err != nil {
		c.notifyRoom(roomID, err.Error(), "red")
		return
	}
	c.notifyRoom(roomID, preset, "purple")
}

func (c *Context) deletePreset(w http.ResponseWriter, r *http.Request) {
	log.Println("Received DELETEPRESET request")
	payLoad, err := util.DecodePostJSON(r, true)
	if err != nil {
		log.Fatalf("Failed to parse payload: " + err.Error())
		return
	}
	roomID := getRoomID(payLoad)

	isAuth, err := isAuthUser(payLoad)
	if err != nil {
		c.notifyRoom(roomID, err.Error(), "red")
	}
	if !isAuth {
		c.notifyRoom(roomID, "You must be an authorized user to do this", "red")
		return
	}

	var entry string = getMessage(payLoad)

	command := strings.SplitN(entry, " ", 3)

	if len(command) < 3 {
		c.notifyRoom(roomID, "deletePreset must follow syntax deletePreset <jobName> <presetName>", "red")
		return
	}
	log.Printf("Deleting job: %s  preset: %s\n", command[1], command[2])
	err = stewql.DeletePreset(command[1], command[2])
	if err != nil {
		c.notifyRoom(roomID, "Problem deleting preset: "+err.Error(), "red")
		return
	}
	_, err = stewql.ListPreset(command[1], command[2])
	if err != nil {
		if err.Error() == "Preset not found" {
			c.notifyRoom(roomID, "Preset successfully deleted", "green")
			return
		} else {
			c.notifyRoom(roomID, "Other error occurred while checking for deletion: "+err.Error(), "red")
			return
		}
	}
	c.notifyRoom(roomID, "Deleting preset unexpectedly failed; preset still found in db", "red")
	return
}

func (c *Context) updatePreset(w http.ResponseWriter, r *http.Request) {
	log.Println("Received UPDATEPRESET request")
	payLoad, err := util.DecodePostJSON(r, true)
	if err != nil {
		log.Fatalf("Failed to parse payload: %s", err.Error())
		return
	}
	roomID := getRoomID(payLoad)

	isAuth, err := isAuthUser(payLoad)
	if err != nil {
		c.notifyRoom(roomID, err.Error(), "red")
	}
	if !isAuth {
		c.notifyRoom(roomID, "You must be an authorized user to do this", "red")
		return
	}

	var entry string = getMessage(payLoad)
	command := strings.SplitN(entry, " ", 4)

	log.Println("Request:")
	log.Println(r)
	log.Println(payLoad)

	if len(command) < 4 {
		c.notifyRoom(roomID, "updatePreset must follow syntax: '!updatePreset <jobName> <presetName> <json>'", "red")
		return
	} else {
		err = stewql.UpdatePreset(command[1], command[2], command[3])
		if err != nil {
			c.notifyRoom(roomID, "Problem updating entry: "+err.Error(), "red")
			return
		}
	}
	preset, err := stewql.ListPreset(command[1], command[2])
	if err != nil {
		c.notifyRoom(roomID, "Error while checking updated preset: "+err.Error(), "red")
		return
	}
	presetArray := strings.SplitN(preset, " ; ", 4)
	presetConfig := strings.SplitN(presetArray[3], " ", 3)
	if command[3] == presetConfig[1] {
		c.notifyRoom(roomID, "Preset successfully updated", "green")
		return
	}
	c.notifyRoom(roomID, "Updated JSON from db does not match new json", "yellow")
}

func (c *Context) config(w http.ResponseWriter, r *http.Request) {
	signedRequest := r.URL.Query().Get("signed_request")
	lp := path.Join("./static", "layout.hbs")
	fp := path.Join("./static", "config.hbs")
	vals := map[string]string{
		"LocalBaseUrl":  c.baseURL,
		"SignedRequest": signedRequest,
		"HostScriptUrl": c.baseURL,
	}
	tmpl, err := template.ParseFiles(lp, fp)
	if err != nil {
		log.Fatalf("%v", err)
	}
	tmpl.ExecuteTemplate(w, "layout", vals)
}

func (c *Context) build(w http.ResponseWriter, r *http.Request) {
	log.Println("Received BUILD request")
	payLoad, err := util.DecodePostJSON(r, true)
	if err != nil {
		log.Println("Something went wrong with the payload: %s\n", err)
		return
	}
	roomID := getRoomID(payLoad)

	isAuth, err := isAuthUser(payLoad)
	if err != nil {
		c.notifyRoom(roomID, err.Error(), "red")
	}
	if !isAuth {
		c.notifyRoom(roomID, "You must be an authorized user to do this", "red")
		return
	}

	log.Println("Request:")
	log.Println(r)
	log.Println(payLoad)

	var entry string = getMessage(payLoad)
	command := strings.SplitN(entry, " ", 3)

	if len(command) < 2 {
		c.notifyRoom(roomID, "Must include jobname: <i>!build <jobName> [jobConfig]</i>", "red")
	} else {
		if len(command) > 2 {
			if strings.Contains(command[2], ":") {
				err := stutil.BuildJob(command[1], command[2])
				if err != nil {
					c.notifyRoom(roomID, "Error while building "+command[1]+": "+err.Error(), "red")
					return
				} else {
					c.notifyRoom(roomID, "Building "+command[1]+" with config "+command[2], "green")
					return
				}
			} else {
				json, err := stewql.GetPresetJson(command[1], command[2])
				if err != nil {
					c.notifyRoom(roomID, "Problem with fetching preset: "+err.Error(), "red")
					return
				}
				err = stutil.BuildJob(command[1], json)
				if err != nil {
					c.notifyRoom(roomID, "There was a problem building the job with the fetched json: "+err.Error(), "red")
					return
				}
				c.notifyRoom(roomID, "Building "+command[1]+" with preset "+json, "green")
				return
			}
		} else {
			err := stutil.BuildJob(command[1], "")
			if err != nil {
				c.notifyRoom(roomID, "Error while building "+command[1]+": "+err.Error(), "red")
				return
			} else {
				c.notifyRoom(roomID, "Building "+command[1]+" with default config", "green")
				return
			}
		}
	}
}

func (c *Context) rebuild(w http.ResponseWriter, r *http.Request) {
	log.Println("Received REBUILD request")
	payLoad, err := util.DecodePostJSON(r, true)
	if err != nil {
		log.Printf("Something went wrong with the payload: %s\n", err)
		return
	}
	log.Println("Request:")
	log.Println(r)
	roomID := getRoomID(payLoad)

	isAuth, err := isAuthUser(payLoad)
	if err != nil {
		c.notifyRoom(roomID, err.Error(), "red")
	}
	if !isAuth {
		c.notifyRoom(roomID, "You must be an authorized user to do this", "red")
		return
	}

	var entry string = getMessage(payLoad)
	command := strings.Split(entry, " ")
	log.Println(command)
	if len(command) < 2 {
		c.notifyRoom(roomID, "Must include jobname: <i>!rebuild <jobName></i>", "red")
	} else {
		stutil.RebuildJob(command[1])
		c.notifyRoom(roomID, "Rebuilding last "+command[1], "green")
	}
}

func (c *Context) getJobConfig(w http.ResponseWriter, r *http.Request) {
	log.Println("In getJobConfig")
	payLoad, err := util.DecodePostJSON(r, true)
	if err != nil {
		log.Printf("Error decoding payload: %s\n", err.Error())
	}
	roomID := getRoomID(payLoad)

	isAuth, err := isAuthUser(payLoad)
	if err != nil {
		c.notifyRoom(roomID, err.Error(), "red")
		//Will this need a return?  Or will !isAuth catch the undefined isAuth
	}
	if !isAuth {
		c.notifyRoom(roomID, "You must be an authorized user to do this", "red")
		return
	}

	log.Println("Request:")
	log.Println(r)
	var entry string = getMessage(payLoad)
	command := strings.Split(entry, " ")
	config, err := stutil.GetJobConfig(command[1])
	if err != nil {
		c.notifyRoom(roomID, "There was a problem with getting the jobConfig: "+err.Error(), "red")
		log.Println(err.Error())
		return
	}

	c.notifyRoom(roomID, config, "purple")
}

func (c *Context) addAuthUser(w http.ResponseWriter, r *http.Request) {
	log.Println("Received ADDAUTHERUSER request")
	payLoad, err := util.DecodePostJSON(r, true)
	if err != nil {
		log.Fatalf("Parsed auth data failed: %v\n", err)
	}
	roomID := getRoomID(payLoad)

	if isAdmin, err := stewql.IsAdmin(getUserId(payLoad)); !isAdmin {
		if err != nil {
			c.notifyRoom(roomID, err.Error(), "red")
			log.Println("IsAdmin error")
			return
		}
		c.notifyRoom(roomID, "You must be an admin to add users", "red")
		return
	}
	if err != nil {
		c.notifyRoom(roomID, err.Error(), "red")
		return
	}
	message := getMessage(payLoad)
	command := strings.SplitN(message, " ", 4)
	log.Println(command)
	switch len(command) {
	case 4:
		c.notifyRoom(roomID, "Must follow syntax: !addUser <userId> <i>BeAdmin?</i>", "red")
		return
	case 3:
		var id string
		if command[2] == "1" || command[2] == "true" {
			id, err = stewql.AddUser(command[1], true)
			if err != nil {
				c.notifyRoom(roomID, err.Error(), "red")
				return
			}
		} else if command[2] == "0" || command[2] == "false" {
			id, err = stewql.AddUser(command[1], false)
			if err != nil {
				c.notifyRoom(roomID, err.Error(), "red")
				return
			}
		} else {
			c.notifyRoom(roomID, "Invalid selection for admin; must be true or false", "yellow")
			return
		}
		isAuthUser, err := stewql.IsAuthUser(command[1])
		if err != nil {
			c.notifyRoom(roomID, err.Error(), "red")
			return
		}
		if isAuthUser {
			c.notifyRoom(roomID, "User has been added: uid: "+id, "green")
			return
		} else {
			c.notifyRoom(roomID, "User has not been successfully entered.  Investigate", "yellow")
			return
		}
	case 2:
		id, err := stewql.AddUser(command[1], false)
		if err != nil {
			c.notifyRoom(roomID, err.Error(), "red")
			return
		}
		isAuthUser, err := stewql.IsAuthUser(command[1])
		if err != nil {
			c.notifyRoom(roomID, err.Error(), "red")
			return
		}
		if isAuthUser {
			c.notifyRoom(roomID, "User has been added: uid: "+id, "green")
			return
		} else {
			c.notifyRoom(roomID, "User has not been successfully entered.  Investigate", "yellow")
			return
		}
	default:
		c.notifyRoom(roomID, "Syntax error: Must follow syntax !addUser <userId> <i><BeAdmin?</i>", "yellow")
	}
}

func (c *Context) help(w http.ResponseWriter, r *http.Request) {
	payLoad, err := util.DecodePostJSON(r, true)
	if err != nil {
		log.Fatalf("parsed auth data failed: %v\n", err)
	}

	roomID := getRoomID(payLoad)

	commands := "Available Commands:<br/>!help <i>This menu</i><br/>!build <jobName> <i><configJSON> || <presetName></i> Builds jobName with Config JSON/named preset as the configuration<br/>!rebuild <jobName> Builds jobName with the last used configuration<br/>!getJobConfig <jobName> Returns the default job config for jobName<br/>!addPreset <presetName> <presetJobConfig> Adds a preset with presetName and a JSON config<br/>!listPreset <presetName> Returns the presetConfig for presetName<br/>!updatePreset <presetName> <newPresetConfig> Updates preset with new config<br/>!getUserId - Returns asking user's hipchat ID for use in addUser<br/>!addUser <userId> <i><beAdmin></i> Adds user with userId as an authorized user; beAdmin is a boolean that sets the user as an Admin or not"

	c.notifyRoom(roomID, commands, "purple")
}

func (c *Context) hook(w http.ResponseWriter, r *http.Request) {
	payLoad, err := util.DecodePostJSON(r, true)
	if err != nil {
		log.Fatalf("Parsed auth data failed: %v\n", err)
	}
	log.Println(payLoad)
	log.Println(r)

	roomID := getRoomID(payLoad)

	util.PrintDump(w, r, true)

	log.Printf("Sending notification to %s\n", roomID)
	c.notifyRoom(roomID, "success! I think", "green")
}

func (c *Context) routes() *mux.Router {
	r := mux.NewRouter()
	//healthcheck route required by Micros
	r.Path("/healthcheck").Methods("GET").HandlerFunc(c.healthcheck)
	//descriptor for Atlassian Connect
	r.Path("/").Methods("GET").HandlerFunc(c.atlassianConnect)

	r.Path("/getUserId").Methods("POST").HandlerFunc(c.getUserId)
	r.Path("/help").Methods("POST").HandlerFunc(c.help)

	//stutil methods
	r.Path("/build").Methods("POST").HandlerFunc(c.build)
	r.Path("/rebuild").Methods("POST").HandlerFunc(c.rebuild)
	r.Path("/getJobConfig").Methods("POST").HandlerFunc(c.getJobConfig)

	//stewql methods
	r.Path("/addPreset").Methods("POST").HandlerFunc(c.addPreset)
	r.Path("/listPreset").Methods("POST").HandlerFunc(c.listPreset)
	r.Path("/deletePreset").Methods("POST").HandlerFunc(c.deletePreset)
	r.Path("/updatePreset").Methods("POST").HandlerFunc(c.updatePreset)
	r.Path("/addUser").Methods("POST").HandlerFunc(c.addAuthUser)

	//HipChat specific API Routes
	r.Path("/installed").Methods("POST").HandlerFunc(c.installable)
	r.Path("/config").Methods("GET").HandlerFunc(c.config)
	r.Path("/hook").Methods("POST").HandlerFunc(c.hook)

	r.PathPrefix("/").Handler(http.FileServer(http.Dir(c.static)))

	return r
}

func (c *Context) notifyRoom(roomID string, message string, color string) {
	var err error
	notifRq := &hipchat.NotificationRequest{
		Message:       message,
		MessageFormat: "html",
		Color:         hipchat.Color(color),
	}

	if _, ok := c.rooms[roomID]; ok {
		_, err = c.rooms[roomID].hc.Room.Notification(roomID, notifRq)
		if err != nil {
			log.Printf("Failed to notifiy HipChat channel: %v\n", err)
		}
	} else {
		log.Printf("Room is not registered correctly:%v\n", c.rooms)
	}
}

func main() {
	defer stewql.Close()
	var (
		port    = flag.String("port", "8080", "Web server port")
		static  = flag.String("static", "./static/", "static folder")
		baseUrl = flag.String("baseurl", os.Getenv("BASE_URL"), "local base url")
	)
	flag.Parse()

	c := &Context{
		baseURL: *baseUrl,
		static:  *static,
		rooms:   make(map[string]*RoomConfig),
	}

	log.Printf("Base Hipchat integration has started!")
	stutil.LogInToJenkins()

	r := c.routes()
	http.Handle("/", r)
	http.ListenAndServe(":"+*port, nil)
}
