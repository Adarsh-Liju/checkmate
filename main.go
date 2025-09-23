// main.go
package main

import (
	"embed"
	"html/template"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

//go:embed templates/*
var templatesFS embed.FS

// Task model represents a task in the system
type Task struct {
	ID          uint       `gorm:"primaryKey" json:"id"`
	Title       string     `json:"title" binding:"required"`
	Description string     `json:"description"`
	Status      string     `json:"status"`
	DueDate     *time.Time `json:"due_date"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// Allowed statuses
var allowedStatuses = map[string]bool{
	"pending":     true,
	"in_progress": true,
	"done":        true,
}

func main() {
	// read DB path from env
	dbPath := os.Getenv("TASK_DB_PATH")
	if dbPath == "" {
		dbPath = "tasks.db"
	}

	// open sqlite db with gorm
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		log.Fatalf("failed to open db: %v", err)
	}

	// automigrate
	if err := db.AutoMigrate(&Task{}); err != nil {
		log.Fatalf("auto migrate failed: %v", err)
	}

	// create gin router
	r := gin.Default()

	// Parse templates from embedded filesystem
	tmpl, err := template.New("").Funcs(template.FuncMap{
		"formatDate": func(t *time.Time) string {
			if t == nil {
				return ""
			}
			return t.Format("2006-01-02")
		},
	}).ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		log.Fatalf("failed to parse templates: %v", err)
	}
	r.SetHTMLTemplate(tmpl)

	// CORS - allow all for dev (adjust in prod)
	r.Use(cors.Default())

	// JSON API routes (preserve existing)
	r.POST("/tasks", func(c *gin.Context) { createTaskHandler(c, db) })
	r.GET("/tasks", func(c *gin.Context) { listTasksHandler(c, db) })
	r.GET("/tasks/:id", func(c *gin.Context) { getTaskHandler(c, db) })
	r.PUT("/tasks/:id", func(c *gin.Context) { updateTaskHandler(c, db) })
	r.PATCH("/tasks/:id", func(c *gin.Context) { patchTaskHandler(c, db) })
	r.DELETE("/tasks/:id", func(c *gin.Context) { deleteTaskHandler(c, db) })
	r.POST("/tasks/:id/complete", func(c *gin.Context) { completeTaskHandler(c, db) })
	r.POST("/add", func(c *gin.Context) { createTaskFormHandlerClassic(c, db) })

	// simple health
	r.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	r.GET("/", func(c *gin.Context) { renderIndex(c, db) })
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("listening on :%s, using DB: %s", port, dbPath)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("server failed: %v", err)
	}
	log.Printf("The website is running at http://localhost:%s/", port)
}

// ---------- (Existing JSON handlers below) ----------
// createTaskHandler creates a new task
func createTaskHandler(c *gin.Context, db *gorm.DB) {
	var payload Task
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// default status
	if payload.Status == "" {
		payload.Status = "pending"
	} else if !allowedStatuses[payload.Status] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid status"})
		return
	}

	if err := db.Create(&payload).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to create task"})
		return
	}

	c.JSON(http.StatusCreated, payload)
}

// listTasksHandler lists tasks with simple filters and pagination
func listTasksHandler(c *gin.Context, db *gorm.DB) {
	// pagination
	pageStr := c.DefaultQuery("page", "1")
	limitStr := c.DefaultQuery("limit", "20")
	page, _ := strconv.Atoi(pageStr)
	limit, _ := strconv.Atoi(limitStr)
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	// filters
	status := c.Query("status")
	q := c.Query("q")

	var tasks []Task
	dbQuery := db.Model(&Task{})
	if status != "" {
		dbQuery = dbQuery.Where("status = ?", status)
	}
	if q != "" {
		like := "%" + q + "%"
		dbQuery = dbQuery.Where("title LIKE ? OR description LIKE ?", like, like)
	}

	var total int64
	dbQuery.Count(&total)

	if err := dbQuery.Order("created_at DESC").Limit(limit).Offset(offset).Find(&tasks).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query tasks"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"page": page, "limit": limit, "total": total, "tasks": tasks})
}

// getTaskHandler returns a single task
func getTaskHandler(c *gin.Context, db *gorm.DB) {
	id := c.Param("id")
	var task Task
	if err := db.First(&task, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}
	c.JSON(http.StatusOK, task)
}

// updateTaskHandler replaces a task
func updateTaskHandler(c *gin.Context, db *gorm.DB) {
	id := c.Param("id")
	var existing Task
	if err := db.First(&existing, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}

	var payload Task
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if payload.Status != "" && !allowedStatuses[payload.Status] {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid status"})
		return
	}

	existing.Title = payload.Title
	existing.Description = payload.Description
	if payload.Status != "" {
		existing.Status = payload.Status
	}
	existing.DueDate = payload.DueDate

	if err := db.Save(&existing).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update task"})
		return
	}
	c.JSON(http.StatusOK, existing)
}

// patchTaskHandler patches fields on a task
func patchTaskHandler(c *gin.Context, db *gorm.DB) {
	id := c.Param("id")
	var existing Task
	if err := db.First(&existing, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}

	var payload map[string]interface{}
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if s, ok := payload["status"].(string); ok {
		if !allowedStatuses[s] {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid status"})
			return
		}
	}

	// ensure we don't update ID/CreatedAt
	delete(payload, "id")
	delete(payload, "created_at")
	if err := db.Model(&existing).Updates(payload).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to patch task"})
		return
	}

	if err := db.First(&existing, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to refetch task"})
		return
	}
	c.JSON(http.StatusOK, existing)
}

// deleteTaskHandler deletes a task
func deleteTaskHandler(c *gin.Context, db *gorm.DB) {
	id := c.Param("id")
	if err := db.Delete(&Task{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete task"})
		return
	}
	c.Status(http.StatusNoContent)
}

// completeTaskHandler marks a task as done
func completeTaskHandler(c *gin.Context, db *gorm.DB) {
	id := c.Param("id")
	var task Task
	if err := db.First(&task, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "task not found"})
		return
	}
	task.Status = "done"
	if err := db.Save(&task).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to complete task"})
		return
	}
	c.JSON(http.StatusOK, task)
}

// renderIndex renders the index page with tasks
func renderIndex(c *gin.Context, db *gorm.DB) {
	var tasks []Task
	db.Order("created_at DESC").Limit(50).Find(&tasks)
	c.HTML(http.StatusOK, "index.html", gin.H{
		"Tasks": tasks,
	})
}

// createTaskFormHandlerClassic handles task creation via form (HTML)
func createTaskFormHandlerClassic(c *gin.Context, db *gorm.DB) {
	title := c.PostForm("title")
	description := c.PostForm("description")
	dueDateStr := c.PostForm("due_date")
	status := "pending"

	var due *time.Time
	if dueDateStr != "" {
		t, err := time.Parse("2006-01-02", dueDateStr)
		if err == nil {
			due = &t
		}
	}

	task := Task{
		Title:       title,
		Description: description,
		Status:      status,
		DueDate:     due,
	}
	if err := db.Create(&task).Error; err != nil {
		c.String(http.StatusInternalServerError, "failed to create task")
		return
	}
	c.Redirect(http.StatusSeeOther, "/")
}
