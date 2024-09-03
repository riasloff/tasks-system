package logger

import (
	"os"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var Log = zap.NewNop()

func Setup() {

	// Конфигурируем запись в консоль
	consoleEncoder := zapcore.NewJSONEncoder(zap.NewDevelopmentEncoderConfig()) // NewConsoleEncoder
	consoleCore := zapcore.NewCore(consoleEncoder, zapcore.AddSync(os.Stdout), zapcore.DebugLevel)

	// Объединяем оба Core с помощью zapcore.Tee
	core := zapcore.NewTee(consoleCore)

	// Создаем логгер на основе объединенного core
	logger := zap.New(core)

	Log = logger
}
