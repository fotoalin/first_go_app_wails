package main

import (
	"database/sql"
	"embed"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"sync"

	_ "github.com/mattn/go-sqlite3"
)

// Embed the frontend directory
//
//go:embed frontend/*
var assets embed.FS

type Task struct {
	ID        int64
	Task      string
	Completed bool
}

type App struct {
	mu        sync.Mutex
	db        *sql.DB
	templates *template.Template
}

func (application *App) initializeDB() error {
	var err error
	application.db, err = sql.Open("sqlite3", "./tasks.db")
	if err != nil {
		return err
	}

	_, err = application.db.Exec(`CREATE TABLE IF NOT EXISTS tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		task TEXT NOT NULL,
		completed BOOLEAN NOT NULL DEFAULT 0
	)`)
	return err
}

func (application *App) AddTask(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(response, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	err := request.ParseForm()
	if err != nil {
		http.Error(response, "Error parsing form: "+err.Error(), http.StatusBadRequest)
		return
	}

	task := request.FormValue("task")
	if task == "" {
		http.Error(response, "Task cannot be empty", http.StatusBadRequest)
		return
	}

	application.mu.Lock()
	_, err = application.db.Exec("INSERT INTO tasks (task) VALUES (?)", task)
	application.mu.Unlock()

	if err != nil {
		http.Error(response, "Error adding task: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Only render the task list template after successful insertion
	application.renderTasks(response, false)
}

func (application *App) GetTasks(w http.ResponseWriter, r *http.Request) {
	fmt.Println("GetTasks called")
	application.renderTasks(w, false)
}

func (application *App) GetCompletedTasks(response http.ResponseWriter, request *http.Request) {
	fmt.Println("GetCompletedTasks called")
	application.renderTasks(response, true)
}

func (application *App) CompleteTask(response http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(response, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	err := request.ParseForm()
	if err != nil {
		http.Error(response, "Error parsing form: "+err.Error(), http.StatusBadRequest)
		return
	}

	taskID := request.FormValue("taskId")
	isCompleted := request.FormValue("completed")
	showCompleted := request.FormValue("showCompleted")

	fmt.Printf("TaskID: %s, Completing: %s, ShowCompleted: %s\n", taskID, isCompleted, showCompleted)

	completed := isCompleted == "true"

	application.mu.Lock()
	_, err = application.db.Exec("UPDATE tasks SET completed = ? WHERE id = ?", completed, taskID)
	application.mu.Unlock()

	if err != nil {
		http.Error(response, "Error updating task: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Show the same list we were viewing (completed or uncompleted)
	application.renderTasks(response, showCompleted == "true")
}

// Add Mutex for Safety
func (application *App) renderTasks(response http.ResponseWriter, completed bool) {
	application.mu.Lock()
	defer application.mu.Unlock()

	var rows *sql.Rows
	var err error
	if completed {
		rows, err = application.db.Query("SELECT id, task, completed FROM tasks WHERE completed = 1 ORDER BY id DESC")
	} else {
		rows, err = application.db.Query("SELECT id, task, completed FROM tasks WHERE completed = 0 ORDER BY id DESC")
	}
	if err != nil {
		http.Error(response, "Error fetching tasks: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var task Task
		if err := rows.Scan(&task.ID, &task.Task, &task.Completed); err != nil {
			http.Error(response, "Error scanning task: "+err.Error(), http.StatusInternalServerError)
			return
		}
		tasks = append(tasks, task)
	}

	err = application.templates.ExecuteTemplate(response, "taskList", tasks)
	if err != nil {
		http.Error(response, "Error rendering template: "+err.Error(), http.StatusInternalServerError)
	}
}

func (application *App) handleIndex(responseWriter http.ResponseWriter, request *http.Request) {
	if request.URL.Path != "/" {
		http.NotFound(responseWriter, request)
		return
	}
	err := application.templates.ExecuteTemplate(responseWriter, "index", nil)
	if err != nil {
		http.Error(responseWriter, err.Error(), http.StatusInternalServerError)
	}
}

func (application *App) DeleteTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Error parsing form: "+err.Error(), http.StatusBadRequest)
		return
	}

	taskID := r.FormValue("taskId")
	showCompleted := r.FormValue("showCompleted") == "true"

	application.mu.Lock()
	_, err = application.db.Exec("DELETE FROM tasks WHERE id = ?", taskID)
	application.mu.Unlock()

	if err != nil {
		http.Error(w, "Error deleting task: "+err.Error(), http.StatusInternalServerError)
		return
	}

	application.renderTasks(w, showCompleted)
}

func (application *App) EditTask(responseWriter http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		http.Error(responseWriter, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	err := request.ParseForm()
	if err != nil {
		http.Error(responseWriter, "Error parsing form: "+err.Error(), http.StatusBadRequest)
		return
	}

	taskID := request.FormValue("taskId")
	newTask := request.FormValue("newTask")
	showCompleted := request.FormValue("showCompleted") == "true"

	if newTask == "" {
		http.Error(responseWriter, "Task cannot be empty", http.StatusBadRequest)
		return
	}

	application.mu.Lock()
	_, err = application.db.Exec("UPDATE tasks SET task = ? WHERE id = ?", newTask, taskID)
	application.mu.Unlock()

	if err != nil {
		http.Error(responseWriter, "Error updating task: "+err.Error(), http.StatusInternalServerError)
		return
	}

	application.renderTasks(responseWriter, showCompleted)
}

func main() {
	application := &App{}

	tmpl, err := template.ParseFS(assets,
		"frontend/base.html",
		"frontend/index.html",
		"frontend/taskList.html")
	if err != nil {
		log.Fatal("Error parsing templates:", err)
	}
	application.templates = tmpl

	err = application.initializeDB()
	if err != nil {
		log.Println("Error initializing database:", err.Error())
		return
	}

	http.HandleFunc("/", application.handleIndex) // This must come first
	http.HandleFunc("/addTask", application.AddTask)
	http.HandleFunc("/getTasks", application.GetTasks)
	http.HandleFunc("/getCompletedTasks", application.GetCompletedTasks)
	http.HandleFunc("/completeTask", application.CompleteTask)
	http.HandleFunc("/deleteTask", application.DeleteTask)
	http.HandleFunc("/editTask", application.EditTask)

	log.Println("Starting HTTP server on http://localhost:8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Println("Error starting HTTP server:", err.Error())
	}
	log.Println("HTTP server stopped")
}
