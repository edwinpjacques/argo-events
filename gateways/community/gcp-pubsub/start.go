/*
Copyright 2018 BlackRock, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package pubsub

import (
	"cloud.google.com/go/pubsub"
	"context"
	"github.com/argoproj/argo-events/common"
	"github.com/argoproj/argo-events/gateways"
	"google.golang.org/api/option"
)

// StartEventSource starts the GCP PubSub Gateway
func (ese *GcpPubSubEventSourceExecutor) StartEventSource(eventSource *gateways.EventSource, eventStream gateways.Eventing_StartEventSourceServer) error {
	defer gateways.Recover(eventSource.Name)

	log := ese.Log.WithField(common.LabelEventSource, eventSource.Name)
	ese.Log.Info("operating on event source")

	config, err := parseEventSource(eventSource.Data)
	if err != nil {
		log.WithError(err).Info("failed to parse event source")
		return err
	}
	sc := config.(*pubSubEventSource)

	ctx := eventStream.Context()

	dataCh := make(chan []byte)
	errorCh := make(chan error)
	doneCh := make(chan struct{}, 1)

	go ese.listenEvents(ctx, sc, eventSource, dataCh, errorCh, doneCh)

	return gateways.HandleEventsFromEventSource(eventSource.Name, eventStream, dataCh, errorCh, doneCh, ese.Log)
}

func (ese *GcpPubSubEventSourceExecutor) listenEvents(ctx context.Context, sc *pubSubEventSource, eventSource *gateways.EventSource, dataCh chan []byte, errorCh chan error, doneCh chan struct{}) {
	// Create a new topic with the given name.
	logger := ese.Log.WithField(common.LabelEventSource, eventSource.Name).WithField("topic", sc.Topic)

	client, err := pubsub.NewClient(ctx, sc.ProjectID, option.WithCredentialsFile(sc.CredentialsFile))
	if err != nil {
		errorCh <- err
		return
	}

	logger.Info("creating GCP PubSub topic")
	topic, err := client.CreateTopic(ctx, sc.Topic)
	if err != nil {
		errorCh <- err
		return
	}

	logger.Info("subscribing to GCP PubSub topic")
	sub, err := client.CreateSubscription(ctx, eventSource.Id,
		pubsub.SubscriptionConfig{Topic: topic})
	if err != nil {
		errorCh <- err
		return
	}

	err = sub.Receive(ctx, func(msgCtx context.Context, m *pubsub.Message) {
		logger.Info("received GCP PubSub Message from topic")
		dataCh <- m.Data
		m.Ack()
	})
	if err != nil {
		errorCh <- err
		return
	}

	<-doneCh

	// after this point, panic on errors
	logger.Info("deleting GCP PubSub subscription")
	if err = sub.Delete(context.Background()); err != nil {
		panic(err)
	}
	logger.Info("deleting GCP PubSub topic")
	if err = topic.Delete(context.Background()); err != nil {
		panic(err)
	}
	logger.Info("closing GCP PubSub client")
	if err = client.Close(); err != nil {
		panic(err)
	}
}
