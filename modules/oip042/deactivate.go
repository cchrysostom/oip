package oip042

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/azer/logger"
	"github.com/bitspill/oip/datastore"
	"github.com/bitspill/oip/events"
	"gopkg.in/olivere/elastic.v6"
)

var deactivationCommitMutex sync.Mutex

func init() {
	events.Bus.SubscribeAsync("modules:oip:mpCompleted", onMpCompleted, false)
}

func onMpCompleted() {
	deactivationCommitMutex.Lock()
	defer deactivationCommitMutex.Unlock()

	q := elastic.NewBoolQuery().Must(
		elastic.NewTermQuery("meta.complete", false),
		elastic.NewTermQuery("meta.stale", false),
	)
	results, err := datastore.Client().Search(oip042DeactivateIndex).Type("_doc").Query(q).Size(10000).Sort("meta.time", false).Do(context.TODO())
	if err != nil {
		log.Error("elastic search failed", logger.Attrs{"err": err})
		return
	}

	// ToDo: move early abort here

	log.Info("Collecting deactivates to attempt applying", logger.Attrs{"pendingDeactivations": len(results.Hits.Hits)})

	if len(results.Hits.Hits) == 0 {
		// early abort
		return
	}

	for _, v := range results.Hits.Hits {
		var ea elasticOip042Deactivate
		err := json.Unmarshal(*v.Source, &ea)
		if err != nil {
			log.Info("failed to unmarshal elastic hit", logger.Attrs{"err": err, "source": *v.Source, "id": v.Id})
			continue
		}

		// deactivate the artifact
		s := elastic.NewScript("ctx._source.meta.deactivated=true;").Type("inline").Lang("painless")
		up := elastic.NewBulkUpdateRequest().Index(oip042ArtifactIndex).Id(ea.Deactivate.Reference).Type("_doc").Script(s)
		datastore.AutoBulk.Add(up)

		// tag deactivation as completed
		s = elastic.NewScript("ctx._source.meta.complete=true;").Type("inline").Lang("painless")
		up = elastic.NewBulkUpdateRequest().Index(oip042DeactivateIndex).Id(ea.Meta.Txid).Type("_doc").Script(s)
		datastore.AutoBulk.Add(up)
	}
}

type elasticOip042DeactivateInterface struct {
	Deactivate interface{} `json:"deactivate"`
	Meta       OMeta       `json:"meta"`
}

type elasticOip042Deactivate struct {
	Deactivate struct {
		Reference string `json:"reference"`
	} `json:"deactivate"`
	Meta OMeta `json:"meta"`
}