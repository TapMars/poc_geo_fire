package main

import (
	"context"
	"flag"
	"log"
	"time"

	"google.golang.org/grpc"
	//"google.golang.org/grpc/credentials"

	"cloud.google.com/go/firestore"
	pb "tap.mars.org/geo_fire_query/geo_fire_query"
)

var (
	address     = flag.String("tls", "localhost:10000", "Address to connect and test server")
	defaultName = flag.String("default_name", "", "")
	keyFile     = flag.String("key_file", "", "The TLS key file")
	jsonDBFile  = flag.String("json_db_file", "", "A json file containing a list of features")
	port        = flag.Int("port", 10000, "The server port")
	projectID   = flag.String("project_ID", "", "The project ID to be used for Firestore access")
)

var client *firestore.Client

type server struct {
	pb.UnimplementedGeoFireQueryServer
}

func createClient() {
	ctx := context.Background()
	var err error
	client, err = firestore.NewClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("Failed to create client: %v", err)
	}
}

func main() {
	flag.Parse()
	log.Printf("running...")
	conn, err := grpc.Dial(*address, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Fatalf("did not connect: %v", err)
	}
	log.Printf("dialed...")
	defer conn.Close()
	c := pb.NewGeoFireQueryClient(conn)
	// Get a Firestore client.
	log.Print("Connected")

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	r, err := c.GetBusinesses(ctx, &pb.BusinessRequest{GeoPoint: &pb.GeoPoint{Latitude: 30.2651242, Longitude: -97.7308078}, FilterDistance: pb.FilterDistance_None, OrderBy: pb.OrderBy_A_to_Z})
	if err != nil {
		log.Fatalf("could not greet: %v", err)
	}
	for _, s := range r.Businesses {
		log.Print(s)
	}
	//log.Printf("Greeting: %s", r.Businesses)

}
