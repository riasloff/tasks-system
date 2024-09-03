package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"tasks-system/internal/logger"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

type Task struct {
	gorm.Model
	Title    string `json:"title"`
	Assignee string `json:"assignee"`
	Finished bool   `json:"finished"`
	Comment  string `json:"comment"`
}

func getTasksHandler(c *gin.Context) {
	var tasks []Task

	// Get all records
	result := gormDB.Find(&tasks)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
		return
	}

	c.JSON(http.StatusOK, tasks)
}

func postTaskHandler(c *gin.Context) {
	var newTask Task

	// Call BindJSON to bind the received JSON to newAlbum.
	if err := c.BindJSON(&newTask); err != nil {
		return
	}

	// Create
	result := gormDB.Create(&newTask)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
		return
	}

	// Кэширование новой задачи в Redis
	ctx := context.Background()
	cacheTTL := 5 * time.Minute
	cacheKey := fmt.Sprintf("task:%d", newTask.ID) // Создание уникального ключа для кэша на основе ID задачи

	taskJSON, err := json.Marshal(newTask)
	if err != nil {
		logger.Log.Info("не удалось сериализовать задачу для кэширования", zap.Error(err))
	} else {
		err := redisClient.Set(ctx, cacheKey, taskJSON, cacheTTL).Err()
		if err != nil {
			logger.Log.Info("не удалось закэшировать вновь созданную задачу", zap.Error(err))
		}
	}

	c.IndentedJSON(http.StatusCreated, newTask)
}

func getTaskByIDHandler(c *gin.Context) {
	var task Task
	ctx := context.Background()

	taskID := c.Param("id")                    // Получение ID задачи из параметров запроса
	cacheKey := fmt.Sprintf("task:%s", taskID) // Создание ключа для кэша

	// Чтение задачи из кэша
	taskJSON, err := redisClient.Get(ctx, cacheKey).Result()
	if err == nil {
		err = json.Unmarshal([]byte(taskJSON), &task)
		if err != nil {
			logger.Log.Debug("не удалось десериализовать задачу из кэша", zap.Error(err))
			c.JSON(http.StatusInternalServerError, gin.H{"error": "ошибка декодирования данных"})
			return
		}
		logger.Log.Debug("задача прочтена из кэша redis")
		c.JSON(http.StatusOK, task)
		return
	}

	if err == redis.Nil {
		logger.Log.Debug("задача не найдена в кэше redis, читаем из базы данных")

		// Чтение данных из базы данных
		result := gormDB.First(&task, taskID)
		if result.Error != nil {
			logger.Log.Debug("не удалось прочитать задачу из базы данных", zap.Error(result.Error))
			c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
			return
		}

		// Кэширование задачи в Redis после успешного чтения из базы данных
		cacheTTL := 5 * time.Minute
		cacheKey := fmt.Sprintf("task:%d", task.ID) // Создание уникального ключа для кэша на основе ID задачи
		taskJSON, err := json.Marshal(task)
		if err != nil {
			logger.Log.Info("не удалось сериализовать задачу для кэширования", zap.Error(err))
		} else {
			err := redisClient.Set(ctx, cacheKey, taskJSON, cacheTTL).Err()
			if err != nil {
				logger.Log.Info("не удалось закэшировать вновь созданную задачу", zap.Error(err))
			}
		}

		logger.Log.Debug("задача прочтена из запроса к базе данных")
		c.JSON(http.StatusOK, task)
		return

	} else {
		logger.Log.Debug("не удалось прочесть задачу", zap.Error(err))
		c.JSON(http.StatusInternalServerError, gin.H{"error": "ошибка получения данных"})
	}
}

func putTaskByIDHandler(c *gin.Context) {
	var task Task

	// Получаем ID задачи из URI
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"msg": "ID is required"})
		return
	}

	// Ищем задачу по ID
	if err := gormDB.First(&task, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"msg": "Task not found"})
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}

	// Обновляем задачу с новыми данными
	if err := c.BindJSON(&task); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid input"})
		return
	}

	// Сохраняем обновлённую задачу в базе данных
	if err := gormDB.Save(&task).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Формируем ключ для кэша
	ctx := context.Background()
	cacheKey := fmt.Sprintf("task:%s", id)

	// Удаляем кэш после обновления задачи
	err := redisClient.Del(ctx, cacheKey).Err()
	if err != nil {
		logger.Log.Warn("Не удалось удалить кэш после обновления задачи", zap.Error(err))
	}

	c.JSON(http.StatusOK, task)
}

func deleteTaskByIDHandler(c *gin.Context) {
	var task Task

	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"msg": "ID is required"})
		return
	}

	// Delete from db
	result := gormDB.Delete(&task, id)
	if result.Error != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": result.Error.Error()})
		return
	}

	// Формируем ключ для кэша
	ctx := context.Background()
	cacheKey := fmt.Sprintf("task:%s", id)

	// Удаляем кэш после удаления задачи
	err := redisClient.Del(ctx, cacheKey).Err()
	if err != nil {
		logger.Log.Warn("Не удалось удалить кэш после удаления задачи", zap.Error(err))
	}

	c.JSON(http.StatusOK, task)
}
