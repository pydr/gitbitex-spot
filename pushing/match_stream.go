// Copyright 2019 GitBitEx.com
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pushing

import (
	"github.com/gitbitex/gitbitex-spot/matching"
	"github.com/gitbitex/gitbitex-spot/models"
	"github.com/gitbitex/gitbitex-spot/service"
	"github.com/gitbitex/gitbitex-spot/utils"
	"github.com/shopspring/decimal"
	"github.com/siddontang/go-log/log"
	"sync"
	"time"
)

type MatchStream struct {
	productId string
	sub       *subscription
	bestBid   decimal.Decimal
	bestAsk   decimal.Decimal
	tick24h   *models.Tick
	tick30d   *models.Tick
	logReader matching.LogReader
}

var lastTickers = sync.Map{}

func newMatchStream(productId string, sub *subscription, logReader matching.LogReader) *MatchStream {
	s := &MatchStream{
		productId: productId,
		sub:       sub,
		logReader: logReader,
	}

	// load last 24h tick
	tick24h, err := service.GetLastTickByProductId(productId, 24*60)
	if err != nil {
		log.Error(err)
	}
	if tick24h != nil {
		s.tick24h = tick24h
	}

	// load last 30d tick
	tick30d, err := service.GetLastTickByProductId(productId, 30*24*60)
	if err != nil {
		log.Error(err)
	}
	if tick30d != nil {
		s.tick30d = tick30d
	}

	s.logReader.RegisterObserver(s)
	return s
}

func (s *MatchStream) Start() {
	// -1 : read from end
	go s.logReader.Run(0, -1)
}

func (s *MatchStream) OnOpenLog(log *matching.OpenLog, offset int64) {
	// do nothing
}

func (s *MatchStream) OnDoneLog(log *matching.DoneLog, offset int64) {
	// do nothing
}

func (s *MatchStream) OnMatchLog(log *matching.MatchLog, offset int64) {
	// refresh tick for next ticker
	refreshTick(&s.tick24h, 24*60, log)
	refreshTick(&s.tick30d, 30*24*60, log)

	// push match
	s.sub.publish(ChannelMatch.FormatWithProductId(log.ProductId), &MatchMessage{
		Type:         "match",
		TradeId:      log.TradeId,
		Sequence:     log.Sequence,
		Time:         log.Time.Format(time.RFC3339),
		ProductId:    log.ProductId,
		Price:        log.Price.String(),
		Side:         log.Side.String(),
		MakerOrderId: utils.I64ToA(log.MakerOrderId),
		TakerOrderId: utils.I64ToA(log.TakerOrderId),
		Size:         log.Size.String(),
	})

	// push ticker
	ticker := &TickerMessage{
		Type:      "ticker",
		TradeId:   log.TradeId,
		Sequence:  log.Sequence,
		Time:      log.Time.Format(time.RFC3339),
		ProductId: log.ProductId,
		Price:     log.Price.String(),
		Side:      log.Side.String(),
		LastSize:  log.Size.String(),
		BestBid:   s.bestBid.String(),
		BestAsk:   s.bestAsk.String(),
		Open24h:   s.tick24h.Open.String(),
		Low24h:    s.tick24h.Low.String(),
		Volume24h: s.tick24h.Volume.String(),
		Volume30d: s.tick30d.Volume.String(),
	}
	lastTickers.Store(log.ProductId, ticker)
	s.sub.publish(ChannelTicker.FormatWithProductId(log.ProductId), ticker)
}

func getLastTicker(productId string) *TickerMessage {
	ticker, found := lastTickers.Load(productId)
	if !found {
		return nil
	}
	return ticker.(*TickerMessage)
}

func refreshTick(tick **models.Tick, granularity int64, log *matching.MatchLog) {
	tickTime := log.Time.UTC().Truncate(time.Duration(granularity) * time.Minute).Unix()

	if *tick == nil || (*tick).Time != tickTime {
		*tick = &models.Tick{
			Open:        log.Price,
			Close:       log.Price,
			Low:         log.Price,
			High:        log.Price,
			Volume:      log.Size,
			ProductId:   log.ProductId,
			Granularity: granularity,
			Time:        tickTime,
		}
	} else {
		(*tick).Close = log.Price
		(*tick).Low = decimal.Min((*tick).Low, log.Price)
		(*tick).High = decimal.Max((*tick).High, log.Price)
		(*tick).Volume = (*tick).Volume.Add(log.Size)
	}
}
