/*
jupyter notebook --allow-root --ip=0.0.0.0
GOOS=windows GOARCH=386 CC=i686-w64-mingw32-gcc go build srv7.go

export SERVER=240.212.0.67:8080
export SERVER=192.168.0.19:8080

dd if=/dev/zero of=dum2 bs=1M count=10
*/

package main

import (
  "flag"
  "fmt"
  "github.com/yasutakatou/ishell"
  "github.com/gorilla/websocket"
  "golang.org/x/text/encoding/japanese"
  "golang.org/x/text/transform"
  "gopkg.in/ini.v1"
  "io"
  "io/ioutil"
  "log"
  "net"
  "net/http"
  "net/url"
  "os"
  "os/exec"
  "regexp"
  "strconv"
  "strings"
  "sync"
  "time"
)

var ActiveClients = make(map[ClientConn]int)
var ActiveClientsRWMutex sync.RWMutex
var shell=ishell.New()

var readBuffer [][]string
var readFlag bool = false

var master string = ""
var OS int = 1
// 1: Linux / 2: Windows
var remote bool = true
// true: Remote / false: Local
var debug bool = true
var clientsAlert int = 0
var HEARTBEAT int = 5
var HEARTBEATCOUNT int = 5
var autoChecksum string = ""
var pushFilename string= ""
var pushServer string = ""
var syncFilename string= ""
var anycast bool = true
var syncFlag bool = false
var socketUpload string
var BUFFERLIMIT int = 1

type ClientConn struct {
  websocket *websocket.Conn
  clientIP  net.Addr
  live      int
}

func addClient(cc ClientConn) {
  ActiveClientsRWMutex.Lock()
  ActiveClients[cc] = 0
  ActiveClientsRWMutex.Unlock()
}

func deleteClient(cc ClientConn) {
  ActiveClientsRWMutex.Lock()
  delete(ActiveClients, cc)
  ActiveClientsRWMutex.Unlock()
}

func changeClient(cc ClientConn, ccc ClientConn) {
  ActiveClientsRWMutex.Lock()
  delete(ActiveClients, cc)
  ActiveClients[ccc] = 0
  ActiveClientsRWMutex.Unlock()
}

func broadcastMessage(messageType int, message []byte, rflag bool, targ string) {
  if rflag == true {
    if readFlag == true {
      fmt.Println("now command result collecting!")
      return
    }
    readFlag = true
  }

  ActiveClientsRWMutex.RLock()
  defer ActiveClientsRWMutex.RUnlock()

  for client, _ := range ActiveClients {
    if targ == "" || targ == IPtoString(client) {

      if debug == true { 
        if messageType == websocket.TextMessage {
          fmt.Printf("cast: %s -> %s \n", message, client.clientIP)
          err := client.websocket.WriteMessage(messageType, message);
          if err != nil {
            log.Println(err)
          }
        } else {
          for i := 0; i < len(readBuffer); i++ {
            if readBuffer[i][1] == IPtoString(client) {
              if readBuffer[i][0] == "pushstatusok" {
                fmt.Printf("binary: data -> %s \n", client.clientIP)
                err := client.websocket.WriteMessage(messageType, message);
                if err != nil {
                  log.Println(err)
                }
              }
            }
          }
        }
      }
    }
  }
}

func IPtoString(cc ClientConn) (string) {
  string := fmt.Sprintf("%s", cc.clientIP)
  return string
}

func pull(filename string) {
  ActiveClientsRWMutex.RLock()
  defer ActiveClientsRWMutex.RUnlock()

  for client, _ := range ActiveClients {
    if IPtoString(client) == master {
      err := client.websocket.WriteMessage(websocket.TextMessage, []byte("pull " + filename));
      if err != nil {
        log.Println(err)
      }
    }
  }
}

func logoutClient(targ string) {
  count := 0
  cflag := false
  prompt := ">>"

  for client, _ := range ActiveClients {
    if IPtoString(client) == targ {
      err := client.websocket.Close()
      if err != nil {
        log.Println(err)
      }
      deleteClient(client)
      if count > 0 { 
        break
      } else {
        cflag = true
      }
    }
    prompt = IPtoString(client)
    if count > 0 && cflag == true { break }
    count = count + 1
  }

  if len(ActiveClients) == 0 {
    master = ""
    shell.SetPrompt(">>> ")
    return
  }

  master = prompt

  shell.SetPrompt(prompt + "> ")
}

func promptSet(server,char string) {
  if server == "" { 
    shell.SetPrompt(">>> ")
    return
  }

  if len(char) > 0 {
    shell.SetPrompt(server + char + " ")
  } else {
    shell.SetPrompt(server + "> ")
  }
}

func listString(targ string,strings []string) {
  fmt.Println(" -- " + targ + " command --")
  for i := 0; i < len(strings); i++ {
    fmt.Println(strings[i])
  }
}

func clearClient() {
  for client, _ := range ActiveClients {
    client.websocket.Close()
    deleteClient(client)
  }

  readBuffer = nil
  readFlag = false

  shell.SetPrompt(">>> ")
}

func main() {

  _, err := exec.Command(os.Getenv("SHELL"), "-c", "which cksum").Output()
  if err != nil {
    OS = 2
  }

  if OS == 1 {
    fmt.Println(" - - - OS: Linux - - -")
  } else {
    fmt.Println(" - - - OS: Windows - - -")
  }

  BUFFERLIMIT, err := strconv.Atoi(os.Getenv("BUFFERLIMIT"))
  if BUFFERLIMIT < 1 { BUFFERLIMIT = 1 }

  HEARTBEATCOUNT, err := strconv.Atoi(os.Getenv("HEARTBEATCOUNT"))
  if HEARTBEATCOUNT < 1 { HEARTBEATCOUNT = 5 }

  HEARTBEAT, err := strconv.Atoi(os.Getenv("HEARTBEAT"))
  if HEARTBEAT < 1 { HEARTBEAT = 5 }

  fmt.Printf("Bufferlimit: %d HeartbeatCount: %d selfHeartbeat: %d\n",BUFFERLIMIT,HEARTBEATCOUNT,HEARTBEAT)

  DEBUG := os.Getenv("DEBUG")
  if DEBUG == "true" { debug = true }
  fmt.Printf("debug mode: %t\n", debug)

  server := os.Getenv("SERVER")
  if server != "" { 
    socketUpload = "http://" + server + "/"
    clientMain(server)
    os.Exit(0)
  }

  CONFIG := os.Getenv("CONFIG")
  if CONFIG == "" {
    CONFIG = "./config.ini"
  }

  port := os.Getenv("PORT")
  if port == "" { port = "8080" }

  // read .ini =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =

  lock := []string{}
  yesno := []string{}
  notice := []string{}
  alias := []string{}
  checksum := []string{}

  loadOptions := ini.LoadOptions{}
  loadOptions.UnparseableSections = []string{"lock", "yesno","notice","alias","checksum"}

  if Exists(CONFIG) == true {
    cfg, err := ini.LoadSources(loadOptions, CONFIG)
    if err != nil {
        fmt.Printf("Fail to read file: %v", err)
        os.Exit(1)
    }

    for _, v := range regexp.MustCompile("\r\n|\n\r|\n|\r").Split(cfg.Section("lock").Body(), -1) { lock = append(lock, v) }
    for _, v := range regexp.MustCompile("\r\n|\n\r|\n|\r").Split(cfg.Section("yesno").Body(), -1) { yesno = append(yesno, v) }
    for _, v := range regexp.MustCompile("\r\n|\n\r|\n|\r").Split(cfg.Section("notice").Body(), -1) { 
      if optionValid(v, false) == true { notice = append(notice, v) }
    }
    for _, v := range regexp.MustCompile("\r\n|\n\r|\n|\r").Split(cfg.Section("alias").Body(), -1) { 
      if optionValid(v, false) == true { alias = append(alias, v) }
    }
    for _, v := range regexp.MustCompile("\r\n|\n\r|\n|\r").Split(cfg.Section("checksum").Body(), -1) { 
      if optionValid(v, true) == true { checksum = append(checksum, v) }
    }

    //if debug == true { 
    //  listString("lock",lock)
    //  listString("yesno",yesno)
    //  listString("notice",notice)
    //  listString("alias",alias)
    //  listString("checksum",checksum)
    //}
  } else {
    fmt.Printf("not found config: %s\n", CONFIG)
  }

  // - =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =

  fmt.Printf("Server listening on port %s.\n", port)
  go func (){
    err := http.ListenAndServe(":"+port, http.HandlerFunc(handler))
    if err != nil {
      panic(err)
    }
  }()

  // Shell config =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =

  shell.AddCmd(&ishell.Cmd{
    Name: "listConfig",
    Help: "list Config. lock / yesno / notice / alias",
    Func: func(c *ishell.Context) {
      listString("lock",lock)
      listString("yesno",yesno)
      listString("notice",notice)
      listString("alias",alias)
      listString("checksum",checksum)
    },
  })

  shell.AddCmd(&ishell.Cmd{
    Name: "delConfig",
    Help: "del Config. lock / yesno / notice / alias",
    Func: func(c *ishell.Context) {
      if len(c.Args) < 2 {
        fmt.Printf("delConfig Usage: (lock/yesno/notice/alias) (value)\n")
        return
      }

      switch c.Args[0] {
        case "lock":
          lock = removeString(lock, c.Args[1])
        case "yesno":
          yesno = removeString(yesno, c.Args[1])
        case "notice":
          notice = removeString(notice, c.Args[1])
        case "alias":
          alias = removeString(alias, c.Args[1])
        case "checksum":
          checksum = removeString(checksum, c.Args[1])
        default:
          fmt.Printf("delConfig Usage: (lock/yesno/notice/alias) (value)\n", port)
      }
    },
  })

  shell.AddCmd(&ishell.Cmd{
    Name: "addConfig",
    Help: "add Config. lock / yesno / notice / alias",
    Func: func(c *ishell.Context) {
      if len(c.Args) < 2 {
        fmt.Printf("addConfig Usage: (lock/yesno/notice/alias) (value)\n")
        return
      }

      switch c.Args[0] {
        case "lock":
          lock = append(lock, c.Args[1])
        case "yesno":
          yesno = append(yesno, c.Args[1])
        case "notice":
          notice = append(notice, c.Args[1])
        case "alias":
          alias = append(alias, c.Args[1])
        case "checksum":
          checksum = append(checksum, c.Args[1])
        default:
          fmt.Printf("addConfig Usage: (lock/yesno/notice/alias) (value)\n", port)
      }
    },
  })

  shell.AddCmd(&ishell.Cmd{
    Name: "OS",
    Help: "target OS Type Linux / Windows",
    Func: func(c *ishell.Context) {
      switch OS {
        case 1:
          OS = 2
          fmt.Println("switch to -> Windows")
        case 2:
          OS = 1
          fmt.Println("switch to -> Linux")
      }
    },
  })

  shell.AddCmd(&ishell.Cmd{
    Name: "clearBuffer",
    Help: "remote command buffer clear",
    Func: func(c *ishell.Context) {
      readBuffer = nil
      readFlag = false
    },
  })

  shell.AddCmd(&ishell.Cmd{
    Name: "clearClients",
    Help: "clear clients",
    Func: func(c *ishell.Context) {
      clearClient()
    },
  })

  shell.AddCmd(&ishell.Cmd{
    Name: "push",
    Help: "push file for clients. push FILENAME TARGETDIR",
    Func: func(c *ishell.Context) {
      if len(c.Args) < 2 {
        fmt.Printf("push Usage: (local file) (remote file)\n")
        return
      }

      if anycast == false {
        fmt.Println("push file not allow unicast mode.")
        return
      }

      if Exists(c.Args[0]) == true {
        dst := dirValidate(c.Args[0],c.Args[1])
        broadcastMessage(websocket.TextMessage, []byte("push " + c.Args[0] + " " + dst), true, "")
        pushFilename = c.Args[0]
        pushServer = "*"
      } else {
        fmt.Println("push file " + c.Args[0] + " not found.")
      }
    },
  })

  shell.AddCmd(&ishell.Cmd{
    Name: "pull",
    Help: "pull file from master client",
    Func: func(c *ishell.Context) {
      if len(c.Args) < 1 {
        fmt.Printf("pull Usage: (remote file)\n")
        return
      }
      pull(c.Args[0])
    },
  })

  shell.AddCmd(&ishell.Cmd{
    Name: "hosts",
    Help: "clients list",
    Func: func(c *ishell.Context) {
      if debug == true { fmt.Printf("clients: %d readFlag: %t readBuffer: %d\n", len(ActiveClients), readFlag, len(readBuffer)) }
      fmt.Printf("anycast: %t\n", anycast)
      fmt.Printf("heartbeat: %d\n", HEARTBEAT)
      fmt.Printf("clients alert: %d\n", clientsAlert)
      switch OS {
        case 1:
          fmt.Println("target OS -> Linux")
        case 2:
          fmt.Println("target OS -> Windows")
      }
      fmt.Printf("remote execute: %t\n", remote)
      clientList()
    },
  })

  shell.AddCmd(&ishell.Cmd{
    Name: "clientsAlert",
    Help: "client connections low notice. 0 is disable",
    Func: func(c *ishell.Context) {
      if len(c.Args) < 1 {
        fmt.Printf("clientsAlert Usage: (clients count)\n")
        return
      }

      cnt, err := strconv.Atoi(c.Args[0])
      if cnt < 1 {
        fmt.Printf("clientsAlert Usage: (clients count)\n")
        return
      }
      clientsAlert = cnt
      if err != nil {
        log.Fatal(err)
      }
    },
  })

  shell.AddCmd(&ishell.Cmd{
    Name: "logout",
    Help: "logout client",
    Func: func(c *ishell.Context) {
      if len(c.Args) < 1 {
        fmt.Printf("logout Usage: (logout clientip:port *hosts*)\n")
        return
      }
      logoutClient(c.Args[0])
    },
  })

  shell.AddCmd(&ishell.Cmd{
    Name: "switch",
    Help: "switch master",
    Func: func(c *ishell.Context) {
      if len(c.Args) < 1 {
        fmt.Printf("switch Usage: (switch master clientip:port *hosts*)\n")
        return
      }
      changeMaster(c.Args[0])
    },
  })

  shell.AddCmd(&ishell.Cmd{
    Name: "mode",
    Help: "remote or local execute palce switch",
    Func: func(c *ishell.Context) {
      if remote == true {
        remote = false
        promptSet("", "")
      } else {
        remote = true
        if anycast == true {
          promptSet(master, "")
        } else {
          promptSet(master, "#")
        }
      }
      fmt.Printf("remote execute: %t\n", remote)
    },
  })

  shell.AddCmd(&ishell.Cmd{
    Name: "anycast",
    Help: "command run all client mode",
    Func: func(c *ishell.Context) {
      if anycast == true {
        anycast = false
        promptSet(master, "#")
      } else {
        anycast = true
        promptSet(master, "")
      }
      fmt.Printf("anycast: %t\n", anycast)
    },
  })

  shell.AddCmd(&ishell.Cmd{
    Name: "debug",
    Help: "debug mode switch",
    Func: func(c *ishell.Context) {
      if debug == true {
        debug = false
      } else {
        debug = true
      }
      fmt.Printf("debug mode: %t\n", debug)
    },
  })

  shell.AddCmd(&ishell.Cmd{
    Name: "checksum",
    Help: "file checksum by get Broadcast",
    Func: func(c *ishell.Context) {
      if len(c.Args) < 1 {
        fmt.Printf("checksum Usage: (remote file)\n")
        return
      }
      checksumFile(c.Args[0])
    },
  })

  shell.AddCmd(&ishell.Cmd{
    Name: "default",
    Help: "default input is execute command",
    Func: func(c *ishell.Context) {

      doCmd := c.Args[0]
      aliasCmd,checksumCmd := iniCheck(doCmd,lock,yesno,notice,alias,checksum)
      if len(aliasCmd) > 0 {
        doCmd = aliasCmd
      }
      if len(checksumCmd) > 0 {
        cnt, err := strconv.Atoi(checksumCmd)
        if err != nil {
          log.Fatal(err)
        }
        autoChecksum = c.Args[cnt]
      }

      for i := 1; i < len(c.Args); i++ {
        doCmd += " "
        doCmd += c.Args[i]
      }

      if remote == true {
        if len(ActiveClients) > 0 {
          if aliasCmd != "notallowcommand" {
            if anycast == true {
              broadcastMessage(websocket.TextMessage, []byte(doCmd), true, "")
            } else {
              broadcastMessage(websocket.TextMessage, []byte(doCmd), true, master)
            }
          }
        } else {
          fmt.Println("client no connect")
        }
      } else {
        if aliasCmd != "notallowcommand" {
          fmt.Println(string(execmd(doCmd)))
        }
      }
    },
  })

  shell.AddCmd(&ishell.Cmd{
    Name: "sync",
    Help: "sync file from master client",
    Func: func(c *ishell.Context) {
      if len(c.Args) < 1 {
        fmt.Printf("sync Usage: (sync master remote file)\n")
        return
      }

      if anycast == false {
        fmt.Println("sync file not allow unicast mode.")
        return
      }

      if len(ActiveClients) > 1 {
        pull(c.Args[0])
        syncFilename = c.Args[0]
      } else {
        fmt.Println("sync command allow join client > 1")
      }
    },
  })

  shell.Run()

  // - =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =- =

}

func dirValidate(src,dst string) (string) {
  result := ""
  params := strings.Split(dst, "/")

  switch params[(len(params) - 1)] {
    case src:
      result = dst
    case "":
      result = dst + dirLocal(src)
    default:
      result = dst + "/" + dirLocal(src)
  }

  return result
}

func removeString(strings []string, search string) []string {
  result := []string{}
  for _, v := range strings {
    if v != search {
      result = append(result, v)
    }
  }
  return result
}

func optionValid(str string, types bool) bool {
  params := strings.Split(str, ":")

  if len(params) == 2 {
    if len(params[0]) == 0 || len(params[1]) == 0 {
      fmt.Println("skip load config: " + str)
      return false
    }
    if types == true {
      _, err := strconv.Atoi(params[1])
      if err != nil {
        log.Fatal(err)
        fmt.Println("skip load config: " + str)
        return false
      }
    }
  }
  return true
}

func Ask4confirm() bool {
  var s string

  fmt.Printf("(y/N): ")
  _, err := fmt.Scan(&s)
  if err != nil {
    panic(err)
  }

  s = strings.TrimSpace(s)
  s = strings.ToLower(s)

  if s == "y" || s == "yes" {
    return true
  }
  return false
}

func iniCheck(cmd string, lock,yesno,notice,alias,checksum []string) (string,string) {
  for i := 0; i < len(lock); i++ {
    if strings.HasPrefix(cmd, lock[i]) == true {
      fmt.Println("command: " + cmd + " is locked!")
      return "notallowcommand",""
    }
  }

  for i := 0; i < len(yesno); i++ {
    if strings.HasPrefix(cmd, yesno[i]) == true {
      if Ask4confirm() == false { return "notallowcommand","" }
      return "",""
    }
  }

  for i := 0; i < len(notice); i++ {
    params := strings.Split(notice[i], ":")
    if strings.HasPrefix(cmd, params[0]) == true {
      fmt.Println(params[1])
      return "",""
    }
  }

  for i := 0; i < len(alias); i++ {
    params := strings.Split(alias[i], ":")
    if strings.HasPrefix(cmd, params[0]) == true {
      return params[1],""
    }
  }

  for i := 0; i < len(checksum); i++ {
    params := strings.Split(checksum[i], ":")
    if strings.HasPrefix(cmd, params[0]) == true {
      return "",params[1]
    }
  }
  
  return "",""
}

func checksumFile(targ string) {
  if remote == true {
    if len(ActiveClients) > 0 {
      switch OS {
        case 1:
          broadcastMessage(websocket.TextMessage, []byte("cksum " + targ), true, "")
        case 2:
          broadcastMessage(websocket.TextMessage, []byte("certutil -hashfile " + targ + " MD5"), true, "")
      }
    } else {
      fmt.Println("client no connect")
    }
  }
}

func changeMaster(targ string) {
  for client, _ := range ActiveClients {
    if IPtoString(client) == targ {
      prompt := targ
      shell.SetPrompt(prompt + "> ")
      master = prompt
    }
  }
  clientList()
}

func clientList() {
  count := 1
  fmt.Println("   |M|C|       IP       |")

  for client, _ := range ActiveClients {
    fmt.Printf("%3s",strconv.Itoa(count))
    if master == IPtoString(client) {
      fmt.Printf("|*|")
    } else {
      fmt.Printf("| |")
    }
    fmt.Printf("%d|",client.live)
    fmt.Println(client.clientIP)
    count = count + 1
  }
}

func push(messageType int, filename string) {
  ActiveClientsRWMutex.RLock()
  defer ActiveClientsRWMutex.RUnlock()

  contents, err := ioutil.ReadFile(filename)
  if err != nil {
    panic(err)
  }

  if debug == true { fmt.Printf("pushFile: %s pushTarget: %s (%d)\n", filename, pushServer, len(contents)) }

  if pushServer == "*" {
    pushServer = ""
  }
  broadcastMessage(messageType, contents, false, pushServer)
}

func rmFile(command string) {
  var err error

  if debug == true { fmt.Printf("temp file: %s delete.\n", command) }

  switch OS {
    case 1: 
      cmd := exec.Command(os.Getenv("SHELL"), "-c", "rm " + command)
      
      cmd.Stdin = os.Stdin
      cmd.Stdout = os.Stdout
      cmd.Stderr = os.Stderr
      
      cmd.Run()
    case 2:
      _, err = exec.Command("cmd", "/C", "del " + command).Output()
      if err != nil {
        log.Fatal(err)
      }
  }
}

func execmd(command string) []byte {
  var out []byte
  var err error

  switch OS {
    case 1: 
      cmd := exec.Command(os.Getenv("SHELL"), "-c", command)
      
      cmd.Stdin = os.Stdin
      cmd.Stdout = os.Stdout
      cmd.Stderr = os.Stderr
      
      cmd.Run()
    case 2:
      out, err = exec.Command("cmd", "/C",command).Output()
      if err != nil {
        log.Fatal(err)
      }
      fmt.Printf("%s\n", out)
  }
  return out
}

var upgrader = websocket.Upgrader{
  ReadBufferSize:  (BUFFERLIMIT*1024*1024),
  WriteBufferSize: (BUFFERLIMIT*1024*1024),
  CheckOrigin: func(*http.Request) bool {
    return true
  },
}

func handler(wr http.ResponseWriter, req *http.Request) {
  //fmt.Printf("%s | %s %s\n", req.RemoteAddr, req.Method, req.URL)
  if websocket.IsWebSocketUpgrade(req) {
    serveWebSocket(wr, req)
  } else if req.URL.Path == "/upload" {
    filename := req.URL.Query().Get("filename")

    if filename == "" {
      log.Fatal("Filename is empty")
    }

    file, err := os.Create(filename)
    if err != nil {
      panic(err)
    }
    n, err := io.Copy(file, req.Body)
    _ = n
    if err != nil {
      panic(err)
    }

    fmt.Printf("\npull file: " + filename + " done.");
  } 
}

func receiveMsg(ws *websocket.Conn) {
  for {
    _, st, err := ws.ReadMessage()
    if err != nil {
      fmt.Println("err: ", err)
      return
    }

    if string(st) == "pullstatusfail" {
      fmt.Printf("push or sync file " + pushFilename + " fail.")
      pushFilename = ""
      pushServer = ""
      syncFlag = false
      return
    }

    if string(st) != "heartbeatmessage" && string(st) != "pushstatusok" {
      if debug == true || anycast == false {
        fmt.Printf("\n - - %s - - \n", ws.RemoteAddr())
        fmt.Printf("%s - - - - - - \n",string(st))
      }
    }

    if string(st) != "heartbeatmessage" {
      ipaddr := fmt.Sprintf("%s", ws.RemoteAddr())
      readBuffer = append(readBuffer, []string{string(st), ipaddr})
    } 
    
    count := 0
    for client, _ := range ActiveClients {
      ipaddr := fmt.Sprintf("%s", ws.RemoteAddr())
      ipstr := IPtoString(client)
      if ipaddr == ipstr {
        if client.live < HEARTBEATCOUNT { 
          sockCli := ClientConn{client.websocket, client.clientIP, HEARTBEATCOUNT}
          changeClient(client, sockCli)
        }
      }
      count = count + 1
    }
  }
}

func getClient(cnt int) string {
  count := 1
  ret := "NOTFOUND"

  for client, _ := range ActiveClients {
    if cnt == count {
      ret = fmt.Sprintf("%s", client.clientIP)
      return ret
    }
    count = count + 1
  }
  return ret
}

func Exists(filename string) bool {
    _, err := os.Stat(filename)
    return err == nil
}

func masterBuffersearch() (string) {
  for i := 0; i < len(readBuffer); i++ {
    if readBuffer[i][1] == master {
      return readBuffer[i][0]
    }
  }
  return ""
}

func dirLocal(src string) (string) {
  params := strings.Split(src, "/")

  if len(params[(len(params) - 1)]) > 0 {
    return params[(len(params) - 1)]
  }
  return ""
}

func upload(ws *websocket.Conn, filename string) {
  if Exists(filename) == false {
    clientSendMsg(ws, "pullstatusfail")
    return
  }

  file, err := os.Open(filename)
  if err != nil {
	panic(err)
  }
  defer file.Close()

  res, err := http.Post(socketUpload + "upload?filename="+filename, "binary/octet-stream", file)
  if err != nil {
	panic(err)
  }
  defer res.Body.Close()
  message, _ := ioutil.ReadAll(res.Body)
  fmt.Printf(string(message))
}

func sjis_to_utf8(str string) (string, error) {
  ret, err := ioutil.ReadAll(transform.NewReader(strings.NewReader(str), japanese.ShiftJIS.NewDecoder()))
  if err != nil {
    return "", err
  }
  return string(ret), err
}


func clientMain(server string) {  
  fmt.Println("login to " + server + " Press Ctrl+C to quit.")

  socketServer := flag.String("server", server, "server address")

  u := url.URL{Scheme: "ws", Host: *socketServer, Path: "/"}
  ws, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
  if err != nil {
    log.Fatal("dial:", err)
  }
  defer ws.Close()

  ws.SetReadLimit(int64(BUFFERLIMIT * 1024 * 1024))

  go clientReceiveMsg(ws)

  for {
    clientSendMsg(ws, "heartbeatmessage")
    if debug == true { fmt.Printf(".") }

    time.Sleep(time.Duration(HEARTBEAT) * time.Second)
  }

  ws.Close()
  fmt.Println("connection close..")
}


func clientExecmd(command string) []byte {
  var out []byte
  var err error

  switch OS {
    case 1: 
      out, err = exec.Command(os.Getenv("SHELL"), "-c", command + " 2>&1 | tee").Output()
      if err != nil {
        log.Fatal(err)
      }
    case 2:
      out, err = exec.Command("cmd", "/C",command).Output()
      if err != nil {
        log.Fatal(err)
      }
  }
  if debug == true { fmt.Printf(" - - - local exec command - - - \n%s\n", out) }
  return out
}

func clientSendMsg(ws *websocket.Conn, msg string) {
  if debug == true && msg != "heartbeatmessage" { { fmt.Printf("send: %s -> %s \n", []byte(msg), ws.RemoteAddr()) } }
  err := ws.WriteMessage(websocket.TextMessage, []byte(msg))
  if err != nil {
    log.Println(err)
    ws.Close()
    fmt.Println("connection close..")
    os.Exit(0)
  }
}

func clientReceiveMsg(ws *websocket.Conn) {
  pushFilename := ""

  for {
    messageType, message, err := ws.ReadMessage()
    if err != nil {
      break
    }
    
    if messageType == websocket.TextMessage {
      fmt.Println("Receive Command: " + string(message))
      if strings.HasPrefix(string(message), "push ") == true {
        pushFilename = strings.Split(string(message), " ")[2]
        clientSendMsg(ws, "pushstatusok")
      } else if strings.HasPrefix(string(message), "pull ") == true {
        params := strings.Split(string(message), " ")
        upload(ws, params[1])
      } else {
        result := string(clientExecmd(string(message)))
        if OS == 2 {
          result,_ = sjis_to_utf8(result)
        }
        clientSendMsg(ws, result)
      }
    } else {
      if debug == true { fmt.Printf("pushFilename: %s\n", pushFilename) }
      ioutil.WriteFile(pushFilename, message, 0644)
    }
    time.Sleep(time.Duration(HEARTBEAT) * time.Second)
  }
}

func serveWebSocket(wr http.ResponseWriter, req *http.Request) {
  connection, err := upgrader.Upgrade(wr, req, nil)
  if err != nil {
    fmt.Printf("%s | %s\n", req.RemoteAddr, err)
    return
  }
  defer connection.Close()
  fmt.Printf("\n%s | join client!\n", req.RemoteAddr)

  if len(ActiveClients) == 0 {
    master = fmt.Sprintf("%s", req.RemoteAddr)
    shell.SetPrompt(master + ">")
  }

  client := connection.RemoteAddr()
  sockCli := ClientConn{connection, client, HEARTBEATCOUNT}
  addClient(sockCli)

  go receiveMsg(connection)

  for {
    count := 1
    for client, _ := range ActiveClients {
      if client.live == 0 {
        logoutClient(IPtoString(client))
      } else {
        sockCli := ClientConn{client.websocket, client.clientIP, (client.live - 1)}
        changeClient(client, sockCli)
      }
      count = count + 1
    }

    if clientsAlert > 0 && clientsAlert > len(ActiveClients) {
      fmt.Printf("NOTICE: clients low %d / %d\n",len(ActiveClients), clientsAlert)
    }

    if readFlag == false && len(syncFilename) > 0 {
      if Exists(dirLocal(syncFilename)) == true {
        checksumFile(syncFilename)
      }
    }

    if (readFlag == true) {
      if len(ActiveClients) == len(readBuffer) || anycast == false || pushServer != "" {
        readFlag = false
    
        if len(syncFilename) > 0 {
          masterBuffer := masterBuffersearch()
          sflag := 0
          for i := 0; i < len(readBuffer); i++ {
            if masterBuffer != readBuffer[i][0] && readBuffer[i][1] != master  {
              targClient := getClient(i + 1)
              fmt.Printf(" - - - (%d: %s) File Diff!!! and push - - - \n", (i + 1) , targClient)
              if debug == true { fmt.Printf(" cksum: %s\n", readBuffer[i][0]) }

              broadcastMessage(websocket.TextMessage, []byte("push " + dirLocal(syncFilename) + " " + syncFilename), true, readBuffer[i][1])
              pushFilename = syncFilename
              pushServer = readBuffer[i][1]
              syncFlag = true
              sflag = sflag + 1
            }
          }
          if sflag == 0 { 
            fmt.Printf("all file same %d server\n", len(ActiveClients))
            if syncFlag == true { rmFile(dirLocal(pushFilename)) }
          } else { fmt.Printf("diff file %d / %d\n", sflag, len(ActiveClients)) }
          syncFilename = ""
        } else if len(pushFilename) > 0 {
          if syncFlag == true { 
            push(websocket.BinaryMessage, dirLocal(pushFilename))
            rmFile(dirLocal(pushFilename))
          } else {
            push(websocket.BinaryMessage, pushFilename)
          }
          pushFilename = ""
          pushServer = ""
        } else if len(autoChecksum) > 0 {
          checksumFile(autoChecksum)
          autoChecksum = ""
        } else {
          if len(ActiveClients) == 1 || anycast == false {
             if anycast == true { fmt.Printf("not diff: single client.") }
             readBuffer = nil
             readFlag = false
             autoChecksum = ""
          } else {
            //fmt.Printf("master: %d\n",master)
            masterBuffer := masterBuffersearch()
            //fmt.Printf("masterBuffer: %s\n",masterBuffer)
            diffFlag := 0
            for i := 0; i < len(readBuffer); i++ {
              if masterBuffer != readBuffer[i][0] && readBuffer[i][1] != master  {
                targClient := getClient(i + 1)
                fmt.Printf(" - - - Diff!!! - - -\n (%d: %s)\n%s\n", (i + 1), targClient, readBuffer[i])
                diffFlag = diffFlag + 1
              }
            }
            if diffFlag == 0 {
              fmt.Println(" - - - not Diff! - - -")
            } else {
              fmt.Printf(" - - - Total Diff %d / %d - - - \n",diffFlag,len(ActiveClients))
            }
          }
        }
        readBuffer = nil
      }
    }
    time.Sleep(time.Duration(HEARTBEAT) * time.Second)
  }
}
