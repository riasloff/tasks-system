package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"tasks-system/internal/logger"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	_ "github.com/lib/pq"
)

var sqlDB *sql.DB
var gormDB *gorm.DB
var redisClient *redis.Client
var err error

func main() {

	logger.Setup()
	defer logger.Log.Sync()
	logger.Log.Info("инициализация системы логов прошла успешно")

	if err := run(); err != nil {
		logger.Log.Fatal("неизвестная ошибка", zap.Error(err))
	}
}

func run() error {

	gin.SetMode(gin.ReleaseMode)
	router := gin.Default()
	router.GET("/", getTasksHandler)
	router.GET("/tasks", getTasksHandler)
	router.POST("/tasks", postTaskHandler)
	router.GET("/tasks/:id", getTaskByIDHandler)
	router.PUT("/tasks/:id", putTaskByIDHandler)
	router.DELETE("/tasks/:id", deleteTaskByIDHandler)

	router.GET("/ping", func(c *gin.Context) { c.String(200, "pong") })

	srv := &http.Server{
		Addr:    ":8080",
		Handler: router.Handler(),
	}

	errChan := make(chan error, 1)
	go func() { // веб сервер
		logger.Log.Info("сервер успешно запущен", zap.String("port", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- fmt.Errorf("ошибка работы веб сервера: %w", err)
		}
	}()

	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, syscall.SIGINT, syscall.SIGTERM)

	go func() { // подключение к базе данных & пинги

		sqlDB, err = sql.Open("postgres", "host=localhost port=5432 user=postgres password=postgrespassword dbname=golang sslmode=disable TimeZone=Europe/Moscow")
		if err != nil {
			errChan <- fmt.Errorf("ошибка при попытке открытия соединение c базой: %w", err)
		}
		gormDB, err = gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
		if err != nil {
			errChan <- fmt.Errorf("ошибка при попытке открытия соединение c базой: %w", err)
		}

		// // Migrate the schema
		// gormDB.AutoMigrate(&Task{})

		for {
			if err = sqlDB.Ping(); err != nil {
				errChan <- fmt.Errorf("ошибка во время пинга базы данных: %w", err)
			}
			time.Sleep(15 * time.Second)
		}
	}()

	go func() { // подключение к redis & пинги

		redisClient = redis.NewClient(&redis.Options{
			Addr:     "localhost:6379",
			Username: "redisuser",
			Password: "redispassword",
			DB:       0, // use default DB
		})

		for {
			ctx := context.Background()
			if err = redisClient.Ping(ctx).Err(); err != nil {
				errChan <- fmt.Errorf("ошибка во время пинга redis: %w", err)
			}
			time.Sleep(15 * time.Second)
		}
	}()

	select {
	case sig := <-stopChan:
		logger.Log.Info("изящное завершение работы сервера", zap.String("signal", sig.String()))

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return srv.Shutdown(ctx)
	case err := <-errChan:
		return err
	}
}
