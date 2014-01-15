package executor

import (
	"fmt"
	"log"
	"pilosa/config"
	"pilosa/core"
	"pilosa/db"
	"pilosa/query"
	"pilosa/util"

	"github.com/davecgh/go-spew/spew"
)

type Executor struct {
	service *core.Service
	inbox   chan *db.Message
}

func (self *Executor) Init() error {
	log.Println("Starting Executor")
	return nil
}

func (self *Executor) Close() {
	log.Println("Shutting down Executor")
}

func (self *Executor) NewJob(job *db.Message) {
	switch job.Data.(type) {
	case query.CountQueryStep:
		self.service.CountQueryStepHandler(job)
	case query.UnionQueryStep:
		self.service.UnionQueryStepHandler(job)
	case query.IntersectQueryStep:
		self.service.IntersectQueryStepHandler(job)
	case query.CatQueryStep:
		self.service.CatQueryStepHandler(job)
	case query.GetQueryStep:
		self.service.GetQueryStepHandler(job)
	case query.SetQueryStep:
		self.service.SetQueryStepHandler(job)
	default:
		fmt.Println("unknown")
	}
}

type stringSlice []string

func (slice stringSlice) pos(value string) int {
	for p, v := range slice {
		if v == value {
			return p
		}
	}
	return -1
}

func (self *Executor) RunQueryTest(database_name string, pql string) string {
	return pql
}

func (self *Executor) runQuery(database *db.Database, qry *query.Query) {
	process, err := self.service.GetProcess()
	if err != nil {
		spew.Dump(err)
	}
	process_id := process.Id()
	fragment_id := util.SUUID(0)
	destination := db.Location{&process_id, fragment_id}

	spew.Dump("QUERY.ID:", qry.Id)
	query_plan := query.QueryPlanForQuery(database, qry, &destination)
	// loop over the query steps and send to Transport
	for _, qs := range *query_plan {
		msg := new(db.Message)
		msg.Data = qs
		switch step := qs.(type) {
		case query.PortableQueryStep:
			self.service.Transport.Send(msg, step.GetLocation().ProcessId)
		}
	}
}

func (self *Executor) RunPQL(database_name string, pql string) interface{} {
	database := self.service.Cluster.GetOrCreateDatabase(database_name)

	// see if the outer query function is a custom query
	reserved_functions := stringSlice{"get", "set", "union", "intersect", "count"}
	tokens := query.Lex(pql)
	outer_token := tokens[0].Text

	if reserved_functions.pos(outer_token) != -1 {

		qry := query.QueryForTokens(tokens)
		go self.runQuery(database, qry)

		var final interface{}
		final, err := self.service.Hold.Get(qry.Id, 10)
		if err != nil {
			spew.Dump(err)
		}
		return final

	} else {
		plugins_dir := config.GetString("plugins")
		plugins_file := plugins_dir + "/" + outer_token + ".js"
		filter := query.TokensToString(tokens)
		query_list := GetMacro(plugins_file, filter).(query.PqlList)

		for i, _ := range query_list {
			qry := query.QueryForPQL(query_list[i].PQL)
			go self.runQuery(database, qry)
			query_list[i].Id = qry.Id
		}

		// technically, this is blocking on the hold for each query, but it may be ok, since we need them all for the final anyway.
		final_result := make(map[string]interface{})
		for i, _ := range query_list {
			final, err := self.service.Hold.Get(query_list[i].Id, 10)
			if err != nil {
				spew.Dump(err)
			}
			spew.Dump(final)
			final_result[query_list[i].Label] = final
		}

		return final_result
	}

}

func (self *Executor) Run() {
	log.Println("Executor Run...")
}

func NewExecutor(service *core.Service) *Executor {
	return &Executor{service, make(chan *db.Message)}
}