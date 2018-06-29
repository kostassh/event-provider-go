package provider

import (
	"github.com/scorum/event-provider-go/event"
	"github.com/scorum/scorum-go"
	"github.com/scorum/scorum-go/apis/blockchain_history"
	"github.com/scorum/scorum-go/apis/chain"
	"github.com/scorum/scorum-go/transport/http"
	"gitlab.scorum.com/blog/api/common"
	"time"
)

type Options struct {
	SyncInterval time.Duration

	BlocksHistoryMaxLimit  uint32
	LookupAccountsMaxLimit uint32

	ErrorRetryTimeout time.Duration
	ErrorRetryLimit   int
}

type Option func(*Options)

func SyncInterval(interval time.Duration) Option {
	return func(args *Options) {
		args.SyncInterval = interval
	}
}

func BlocksHistoryMaxLimit(limit uint32) Option {
	return func(args *Options) {
		args.BlocksHistoryMaxLimit = limit
	}
}

func LookupAccountsMaxLimit(limit uint32) Option {
	return func(args *Options) {
		args.LookupAccountsMaxLimit = limit
	}
}

func ErrorRetryTimeout(timeout time.Duration) Option {
	return func(args *Options) {
		args.ErrorRetryTimeout = timeout
	}
}

func ErrorRetryLimit(limit int) Option {
	return func(args *Options) {
		args.ErrorRetryLimit = limit
	}
}

type Provider struct {
	Blockchain *scorumgo.Client
	Options    *Options
}

func NewProvider(url string, setters ...Option) *Provider {
	args := &Options{
		SyncInterval: time.Second,

		BlocksHistoryMaxLimit:  100,
		LookupAccountsMaxLimit: 1000,

		ErrorRetryTimeout: 10 * time.Second,
		ErrorRetryLimit:   3,
	}

	for _, setter := range setters {
		setter(args)
	}

	transport := http.NewTransport(url)

	monitor := &Provider{
		Blockchain: scorumgo.NewClient(transport),
		Options:    args,
	}

	return monitor
}

func (p *Provider) Provide(from uint32, types []event.Type, buffer int) (<-chan event.Event, <-chan error) {
	c := make(chan event.Event, buffer)
	e := make(chan error, 1)

	go func() {
		defer close(c)
		defer close(e)

		for {
			properties, err := p.getChainProperties()
			if err != nil {
				e <- err
				return
			}

			if from == properties.HeadBlockNumber {
				time.Sleep(p.Options.ErrorRetryTimeout)
				continue
			}

			// GetBlockHistory has descending order
			limit := properties.HeadBlockNumber - from
			if limit > p.Options.BlocksHistoryMaxLimit {
				limit = p.Options.BlocksHistoryMaxLimit
			}
			offset := from + limit

			history, err := p.getBlockHistory(offset, limit)
			if err != nil {
				e <- err
				return
			}

			for num, block := range history {
				if num > from {
					from = num
				}

				for _, transaction := range block.Transactions {
					for _, operation := range transaction.Operations {
						ev := event.ToEvent(operation, block.BlockID, num)
						for _, eventType := range types {
							if ev.Type() == eventType {
								c <- ev
								break
							}
						}
					}
				}
			}
		}
	}()

	return c, e
}

func (p *Provider) getChainProperties() (prop *chain.ChainProperties, err error) {
	common.TryDo(func(attempt int) (retry bool, err error) {
		prop, err = p.Blockchain.Chain.GetChainProperties()
		if err != nil {
			time.Sleep(p.Options.ErrorRetryTimeout)
		}
		return attempt < p.Options.ErrorRetryLimit, err
	})
	return
}

func (p *Provider) getBlockHistory(blockNum, limit uint32) (history blockchain_history.BlockHistory, err error) {
	common.TryDo(func(attempt int) (retry bool, err error) {
		history, err = p.Blockchain.BlockchainHistory.GetBlocksHistory(blockNum, limit)
		if err != nil {
			time.Sleep(p.Options.ErrorRetryTimeout)
		}
		return attempt < p.Options.ErrorRetryLimit, err
	})
	return
}
