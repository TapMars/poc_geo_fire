package main

import (
	"context"
	"flag"
	"fmt"
	"google.golang.org/api/iterator"
	"google.golang.org/grpc/metadata"
	"log"
	"math"
	"net"
	"sync"
	"time"

	"github.com/uber/h3-go"
	"google.golang.org/grpc"
	//"google.golang.org/grpc/credentials"

	firebase "firebase.google.com/go/v4"

	"cloud.google.com/go/firestore"
	pb "tap.mars.org/geo_fire_query/geo_fire_query"
)

var (
	tls        = flag.Bool("tls", false, "Connection uses TLS if true, else plain TCP")
	certFile   = flag.String("cert_file", "", "The TLS cert file")
	keyFile    = flag.String("key_file", "", "The TLS key file")
	jsonDBFile = flag.String("json_db_file", "", "A json file containing a list of features")
	port       = flag.Int("port", 10000, "The server port")
	projectID  = flag.String("project_ID", "", "The project ID to be used for Firestore access")
	firestoreClient     *firestore.Client
	app 		*firebase.App
)

type server struct {
	pb.UnimplementedGeoFireQueryServer
}

func initializeAppDefault() {
	var err error
	app, err = firebase.NewApp(context.Background(), nil)
	if err != nil {
		log.Fatalf("error initalizing app: %v\n", err)
	}
}

//verifyIDToken
func getUserId(ctx context.Context) string {
	// [START verify_id_token_golang]
	headers, ok := metadata.FromIncomingContext(ctx)
	if ok == false {
		//will update. Don't need to crash app for bad tokens
		log.Fatalf("error getting token header \n")
	}
	idToken := headers["authorization"]

	client, err := app.Auth(ctx)
	if err != nil {
		log.Fatalf("error getting Auth client: %v\n", err)
	}

	token, err := client.VerifyIDToken(ctx, idToken[0])
	if err != nil {
		log.Fatalf("error verifying ID token: %v\n", err)
	}

	log.Printf("Verified ID token: %v\n", token)
	// [END verify_id_token_golang]



	return token.UID
}

func createClient() {
	ctx := context.Background()
	var err error
	firestoreClient, err = firestore.NewClient(ctx, *projectID)
	if err != nil {
		log.Fatalf("Failed to create firestoreClient: %v", err)
	}
}

func toRadians(num float64) float64 {
	return num * math.Pi / float64(180)
}

//calcDistance calculates the distance between two points using the "haversine" formula.
func calcDistance(geo1 *h3.GeoCoord, geo2 *h3.GeoCoord) (mi, km float64) {
	const earthRadiusMi = 3958 // radius of the earth in miles.
	const earthRadiusKm = 6371 // radius of the earth in kilometers.

	lat1 := toRadians(geo1.Latitude)
	lat2 := toRadians(geo2.Latitude)
	lng1 := toRadians(geo1.Longitude)
	lng2 := toRadians(geo2.Longitude)
	diffLat := lat2 - lat1
	diffLon := lng2 - lng1

	a := math.Pow(math.Sin(diffLat/2), 2) + math.Cos(lat1)*math.Cos(lat2)*
		math.Pow(math.Sin(diffLon/2), 2)

	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	mi = c * earthRadiusMi
	km = c * earthRadiusKm
	return mi, km
}

//modifyBusiness updates business object with missing attributes
func modifyBusiness(bus *pb.Business, doc *firestore.DocumentSnapshot, geoOrigin *h3.GeoCoord) {
	bus.Id = doc.Ref.ID
	//Accessing index via DataAt because value is being stripped out from firestoreClient.
	h3index15, err := doc.DataAt("h3index15")
	if err != nil {
		log.Print(err)
	} else {
		//Getting Geo Coordinates from index. Starts as string so has to string -> Index -> Geo
		//Needing to extract it through an "empty interface" and "type assertion"
		geoBus := h3.ToGeo(h3.FromString(h3index15.(string)))
		mi, km := calcDistance(&geoBus, geoOrigin)
		bus.Distance = &pb.Distance{DistanceMi: mi, DistanceKm: km}
	}

}

func queryIndex(ctx context.Context,wg *sync.WaitGroup ,index h3.H3Index, queryResults chan *pb.Business, geoOrigin *h3.GeoCoord) {
	defer wg.Done()

	query := firestoreClient.Collection("businesses").
		Where("h3index7", "==", h3.ToString(index)).
		//OrderBy("name", firestore.Desc).
		Limit(20)
	iter := query.Documents(ctx)
	defer iter.Stop()

	//Runs forever until iterator hits end of list. A break will exit for loop and end function
	//Should capture error into logs? or error channel?
	for {
		doc, err := iter.Next()
		if err == iterator.Done {
			break
		}
		if err != nil {
			//return nil, err
			log.Print(err)
			break
		}
		//Creating new business pointer to populate
		var busData *pb.Business
		//Mapping Firestore Business to pointer Business. Creates new object within pointer
		if err := doc.DataTo(&busData); err != nil {
			//Missing attribute. Don't added to channel
			log.Print(err)
			break
		}
		modifyBusiness(busData, doc, geoOrigin)
		queryResults <- busData
	}

}

func (s *server) GetBusinesses(ctx context.Context, in *pb.BusinessRequest) (*pb.BusinessResponse, error) {
	start := time.Now()



	var businesses []*pb.Business

	geoOrigin := h3.GeoCoord{
		Latitude:  in.GeoPoint.Latitude,
		Longitude: in.GeoPoint.Longitude,
	}
	h3Index := h3.FromGeo(geoOrigin, 7)

	//Get the toughing neighbors of the requested GeoPint
	h3Indexes := h3.KRing(h3Index, 1)

	//A channel to add all the data to from the queries
	queryResults := make(chan *pb.Business)
	//Going to create WaitGroup to kick off query routines
	wg := &sync.WaitGroup{}
	sliceLength := len(h3Indexes)
	wg.Add(sliceLength)

	for _,index := range h3Indexes {
		//	log.Print(index)
			go queryIndex(ctx, wg, index, queryResults, &geoOrigin)
	}
	go func(){
		wg.Wait()
		close(queryResults)
	}()


	for business := range queryResults {
		businesses = append(businesses, business)
	}

	end := time.Now()
	elapsed := end.Sub(start)
	log.Print(elapsed)

	return &pb.BusinessResponse{Businesses: businesses}, nil
}

func main() {
	flag.Parse()

	// Get a Firebase Auth firestoreClient
	initializeAppDefault()

	// Get a Firestore firestoreClient.
	createClient()
	defer firestoreClient.Close()


	lis, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", *port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}

	s := grpc.NewServer()
	pb.RegisterGeoFireQueryServer(s, &server{})
	if err := s.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}

}
