package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	"github.com/go-redis/redis/v8"

	ui "github.com/gizak/termui/v3"
	"github.com/gizak/termui/v3/widgets"
)

var (
	ctx = context.Background()
)

// Fixed Window Algo
func fixedWindowAlgo(client *redis.Client, key string, limit int64, window time.Duration) bool {
	currentTime := time.Now().UnixNano()
	keyWindow := fmt.Sprintf("%s:%d", key, currentTime/window.Nanoseconds())

	count, err := client.Incr(ctx, keyWindow).Result()
	if err != nil {
		panic(err)
	}
	if count > limit {
		return false
	}

	// Expire key after the fixed window duration
	if err := client.Expire(ctx, keyWindow,window).Err(); err != nil {
		panic(err)
	}

	return true
}

//Sliding Log Algo
func slidingLogsAlgo(client *redis.Client, key string, limit int64, window time.Duration) bool {
	currentTime := time.Now().UnixNano()
	keyLogs := fmt.Sprintf("%s_logs", key)
	keyTimestamps := fmt.Sprintf("%s_timestamps", key)

	pipe := client.TxPipeline()
	ctx := context.Background()
	pipe.ZRemRangeByScore(ctx, keyLogs, "0", fmt.Sprintf("%d", currentTime - int64(window)))
	pipe.ZAdd(ctx, keyLogs, &redis.Z{
		Score: float64(currentTime),
		Member: currentTime,
	})
	pipe.ZCard(ctx,keyLogs)
	_, err := pipe.Exec(ctx)
	if err != nil {
		panic(err)
	}

	count, err := client.Get(ctx, keyTimestamps).Int64()
	if err != nil && err != redis.Nil {
		panic(err)
	}
	if count >= limit {
		return false
	}

	pipe = client.TxPipeline()
	pipe.Incr(ctx, keyTimestamps)
	pipe.Expire(ctx, keyTimestamps, window)

	_, err = pipe.Exec(ctx)
	if err != nil {
		panic(err)
	}
	return true

}

func main(){

	client := redis.NewClient( &redis.Options{

		Addr: "localhost:6379",
		DB: 0,
	})

	if err := ui.Init(); err != nil {
		log.Fatalf("failed to initialize termui: %v", err)
	}
	defer ui.Close()

	// Test fixed window
	data := make([]float64, 0)
	fixed_handler := func(w http.ResponseWriter, req *http.Request) {
		if fixedWindowAlgo(client, "fixed_window", 3, time.Second) {
			data = append(data, 1)
		} else {
			data = append(data, 0)
		}
		time.Sleep(time.Millisecond * 100)

		io.WriteString(w, "served request\n")
	}
	
	data2 := make([]float64, 0)
	sliding_handler := func(w http.ResponseWriter, req *http.Request){
		if fixedWindowAlgo(client, "fixed_window", 3, time.Second) {
			data2 = append(data2, 1)
		} else {
			data2 = append(data2, 0)
		}
		time.Sleep(time.Millisecond * 100)

		io.WriteString(w, "served request\n")
	}

	http.HandleFunc("/fixed_window", fixed_handler)
	http.HandleFunc("/sliding_logs", sliding_handler)
	fmt.Println("Starting Server")
	server := &http.Server{Addr:":8080"}
	go func(){
		if err := server.ListenAndServe(); err != nil {
			fmt.Println(err)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)

	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 5 * time.Second)
	defer cancel()
	fmt.Println("Shutting Down Server")
	if err := server.Shutdown(ctx); err != nil {
		fmt.Println(err)
	}

	fmt.Println("Rendering Grap")
	sl0 := widgets.NewSparkline()
	sl0.Title = "Fixed Window"
	sl0.Data = data
	sl0.LineColor = ui.ColorGreen

	sl1 := widgets.NewSparkline()
	sl1.Title = "Sliding Log"
	sl1.Data = data2
	sl1.LineColor = ui.ColorRed

	slg0 := widgets.NewSparklineGroup(sl0, sl1)
	slg0.SetRect(0, 0, 70, 10)

	ui.Render(slg0)

	if err := client.Close(); err != nil {
		panic(err)
	}
	uiEvents := ui.PollEvents()
	for {
		e := <-uiEvents
		switch e.ID {
		case "q", "<C-c>":
			return
		}
	}

}
