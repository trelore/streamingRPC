package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"

	greetv1 "github.com/alexanderjophus/streamingRPC/gen/greet/v1"        // generated by protoc-gen-go
	"github.com/alexanderjophus/streamingRPC/gen/greet/v1/greetv1connect" // generated by protoc-gen-connect-go
	"github.com/bufbuild/connect-go"
	"github.com/jdkato/prose/v2"
	"github.com/rs/cors"
	"github.com/trelore/go-broadcast"
	"golang.org/x/net/http2"
	"golang.org/x/net/http2/h2c"
)

type GreetServer struct {
	b broadcast.Broadcaster[string]
}

func (s *GreetServer) Greet(
	ctx context.Context,
	req *connect.Request[greetv1.GreetRequest],
) (*connect.Response[greetv1.GreetResponse], error) {

	s.b.Submit(req.Msg.GetName())

	res := connect.NewResponse(&greetv1.GreetResponse{
		Greeting: fmt.Sprintf("Hello, %s!", req.Msg.Name),
	})
	return res, nil
}

func (s *GreetServer) GreetStream(
	ctx context.Context,
	req *connect.Request[greetv1.GreetStreamRequest],
	stream *connect.ServerStream[greetv1.GreetStreamResponse],
) error {
	ch := make(chan string)
	s.b.Register(ch)
	defer s.b.Unregister(ch)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case name := <-ch:
			stream.Send(&greetv1.GreetStreamResponse{
				People: name,
			})
		}
	}
}

func (s *GreetServer) ExtractEntities(
	ctx context.Context,
	stream *connect.BidiStream[greetv1.ExtractEntitiesRequest, greetv1.ExtractEntitiesResponse],
) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		request, err := stream.Receive()
		if err != nil && errors.Is(err, io.EOF) {
			return nil
		} else if err != nil {
			return fmt.Errorf("receive request: %v", err)
		}
		doc, err := prose.NewDocument(request.Message)
		if err != nil {
			return fmt.Errorf("not parseable: %v", err)
		}
		ret := []*greetv1.ExtractEntitiesResponse_Result{}
		for _, tok := range doc.Entities() {
			ret = append(ret, &greetv1.ExtractEntitiesResponse_Result{
				Text:  tok.Text,
				Label: tok.Label,
			})
		}
		if err = stream.Send(&greetv1.ExtractEntitiesResponse{
			Results: ret,
		}); err != nil {
			log.Println(err)
		}
	}
}

func newCORS() *cors.Cors {
	return cors.New(cors.Options{
		AllowedMethods: []string{
			http.MethodHead,
			http.MethodGet,
			http.MethodPost,
			http.MethodPut,
			http.MethodPatch,
			http.MethodDelete,
		},
		AllowOriginFunc: func(origin string) bool {
			log.Println(origin)
			return true
		},
		AllowedHeaders: []string{"*"},
		ExposedHeaders: []string{
			"Accept",
			"Accept-Encoding",
			"Accept-Post",
			"Connect-Accept-Encoding",
			"Connect-Content-Encoding",
			"Content-Encoding",
			"Grpc-Accept-Encoding",
			"Grpc-Encoding",
			"Grpc-Message",
			"Grpc-Status",
			"Grpc-Status-Details-Bin",
		},
	})
}

func main() {
	greeter := &GreetServer{
		b: broadcast.NewBroadcaster[string](10),
	}
	defer greeter.b.Close()
	mux := http.NewServeMux()
	path, handler := greetv1connect.NewGreetServiceHandler(greeter)
	mux.Handle(path, handler)
	http.ListenAndServe(
		"localhost:8080",
		// Use h2c so we can serve HTTP/2 without TLS.
		h2c.NewHandler(newCORS().Handler(mux), &http2.Server{}),
	)
}
