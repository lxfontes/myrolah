package main

import (
	"database/sql"
	"errors"
	"flag"
	"net/http"
	"strconv"

	_ "github.com/go-sql-driver/mysql"
)

var (
	ErrNotMaster      = errors.New("Not a master")
	ErrMasterReadOnly = errors.New("DB is read only")
	ErrMasterIsSlave  = errors.New("Master has slave configuration")

	ErrSlaveNotReadOnly   = errors.New("Slave is writeable")
	ErrSlaveIoNotRunning  = errors.New("IO Slave not running")
	ErrSlaveSQLNotRunning = errors.New("SQL Slave not running")
	ErrSlaveLagging       = errors.New("Slave is lagging")
	ErrNotSlave           = errors.New("Not a slave")

	ErrNoRecords = errors.New("No records")
)

type MonitorCtx struct {
	Db       *sql.DB
	SlaveLag int
}

type Monitor struct {
	master bool
	conf   *MonitorCtx
}

func (monitor *Monitor) checkMaster(w http.ResponseWriter, r *http.Request) {
	isMaster, err := monitor.conf.Master()

	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	if !isMaster {
		http.Error(w, ErrNotMaster.Error(), http.StatusBadGateway)
		return
	}
}

func (monitor *Monitor) checkSlave(w http.ResponseWriter, r *http.Request) {
	isSlave, err := monitor.conf.Slave()

	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}

	if !isSlave {
		http.Error(w, ErrNotSlave.Error(), http.StatusBadGateway)
		return
	}
}

func (monitor *Monitor) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if monitor.master {
		monitor.checkMaster(w, r)
	} else {
		monitor.checkSlave(w, r)
	}
}

func NewMonitor(conf *MonitorCtx, master bool) *Monitor {
	m := &Monitor{
		conf:   conf,
		master: master,
	}
	return m
}

func (ctx *MonitorCtx) Slave() (bool, error) {
	var read_only int
	var err error

	err = ctx.Db.QueryRow("SELECT @@global.read_only AS read_only").Scan(&read_only)

	if err != nil {
		return false, err
	}

	if read_only != 1 {
		return false, ErrSlaveNotReadOnly
	}

	rows, err := ctx.Db.Query("SHOW SLAVE STATUS")

	if err != nil {
		return false, err
	}

	defer rows.Close()

	status, err := mapRows(rows)

	if err != nil {
		return false, err
	}

	if status["Slave_IO_Running"] != "Yes" {
		return false, ErrSlaveIoNotRunning
	}

	if status["Slave_SQL_Running"] != "Yes" {
		return false, ErrSlaveSQLNotRunning
	}

	secondsBehind := status["Seconds_Behind_Master"].(string)

	lag, err := strconv.Atoi(secondsBehind)

	if err != nil {
		return false, err
	}

	if lag > ctx.SlaveLag {
		return false, ErrSlaveLagging
	}

	return true, nil
}

func (ctx *MonitorCtx) Master() (bool, error) {
	var read_only int
	var err error

	err = ctx.Db.QueryRow("SELECT @@global.read_only AS read_only").Scan(&read_only)

	if err != nil {
		return false, err
	}

	if read_only > 0 {
		return false, ErrMasterReadOnly
	}

	rows, err := ctx.Db.Query("SHOW SLAVE STATUS")

	if err != nil {
		return false, err
	}

	defer rows.Close()

	if rows.Next() {
		return false, ErrMasterIsSlave
	}

	return true, nil
}

// maps a SINGLE row to a map
func mapRows(rows *sql.Rows) (map[string]interface{}, error) {
	ret := make(map[string]interface{})

	names, err := rows.Columns()
	if err != nil {
		return ret, err
	}

	vals := make([]interface{}, len(names))
	valPtrs := make([]interface{}, len(names))

	for i := range names {
		valPtrs[i] = &vals[i]
	}

	if !rows.Next() {
		return ret, ErrNoRecords
	}

	err = rows.Scan(valPtrs...)

	if err != nil {
		return ret, err
	}

	for i := 0; i < len(names); i++ {
		val := vals[i]
		name := names[i]

		b, ok := val.([]byte)
		if ok {
			ret[name] = string(b)
		}
	}

	return ret, nil
}

var dbUrl = flag.String("url", "root:@tcp(127.0.0.1:3306)/information_schema", "DB URL")
var slaveLag = flag.Int("lag", 30, "Slave Lag")

var masterBind = flag.String("master", ":7555", "Master HTTP Check Port")
var slaveBind = flag.String("slave", ":7556", "Slave HTTP Check Port")

func main() {
	flag.Parse()

	conf := &MonitorCtx{}
	var err error

	conf.SlaveLag = *slaveLag

	conf.Db, err = sql.Open("mysql", *dbUrl)

	if err != nil {
		panic(err.Error())
	}

	masterMonitor := NewMonitor(conf, true)
	slaveMonitor := NewMonitor(conf, false)

	go http.ListenAndServe(*masterBind, masterMonitor)
	go http.ListenAndServe(*slaveBind, slaveMonitor)
	select {}
}
