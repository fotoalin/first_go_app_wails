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

func (a *App) initializeDB() error {
	var err error
	a.db, err = sql.Open("sqlite3", "./tasks.db")
	if err != nil {
		return err
	}

	_, err = a.db.Exec(`CREATE TABLE IF NOT EXISTS tasks (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		task TEXT NOT NULL,
		completed BOOLEAN NOT NULL DEFAULT 0
	)`)
	return err
}

func (a *App) AddTask(response http.ResponseWriter, request *http.Request) {
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

	a.mu.Lock()
	_, err = a.db.Exec("INSERT INTO tasks (task) VALUES (?)", task)
	a.mu.Unlock()

	if err != nil {
		http.Error(response, "Error adding task: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Only render the task list template after successful insertion
	a.renderTasks(response, false)
}

func (a *App) GetTasks(w http.ResponseWriter, r *http.Request) {
	fmt.Println("GetTasks called")
	a.renderTasks(w, false)
}

func (a *App) GetCompletedTasks(response http.ResponseWriter, request *http.Request) {
	fmt.Println("GetCompletedTasks called")
	a.renderTasks(response, true)
}

func (appInstance *App) CompleteTask(response http.ResponseWriter, request *http.Request) {
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

	appInstance.mu.Lock()
	_, err = appInstance.db.Exec("UPDATE tasks SET completed = ? WHERE id = ?", completed, taskID)
	appInstance.mu.Unlock()

	if err != nil {
		http.Error(response, "Error updating task: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Show the same list we were viewing (completed or uncompleted)
	appInstance.renderTasks(response, showCompleted == "true")
}

// Add Mutex for Safety
func (appInstance *App) renderTasks(response http.ResponseWriter, completed bool) {
	appInstance.mu.Lock()
	defer appInstance.mu.Unlock()

	var rows *sql.Rows
	var err error
	if completed {
		rows, err = appInstance.db.Query("SELECT id, task, completed FROM tasks WHERE completed = 1 ORDER BY id DESC")
	} else {
		rows, err = appInstance.db.Query("SELECT id, task, completed FROM tasks WHERE completed = 0 ORDER BY id DESC")
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

	err = appInstance.templates.ExecuteTemplate(response, "taskList", tasks)
	if err != nil {
		http.Error(response, "Error rendering template: "+err.Error(), http.StatusInternalServerError)
	}
}

func (appInstance *App) handleIndex(responseWriter http.ResponseWriter, request *http.Request) {
	if request.URL.Path != "/" {
		http.NotFound(responseWriter, request)
		return
	}
	err := appInstance.templates.ExecuteTemplate(responseWriter, "index", nil)
	if err != nil {
		http.Error(responseWriter, err.Error(), http.StatusInternalServerError)
	}
}

func (appInstance *App) DeleteTask(w http.ResponseWriter, r *http.Request) {
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

	appInstance.mu.Lock()
	_, err = appInstance.db.Exec("DELETE FROM tasks WHERE id = ?", taskID)
	appInstance.mu.Unlock()

	if err != nil {
		http.Error(w, "Error deleting task: "+err.Error(), http.StatusInternalServerError)
		return
	}

	appInstance.renderTasks(w, showCompleted)
}

func (appInstance *App) EditTask(responseWriter http.ResponseWriter, request *http.Request) {
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

	appInstance.mu.Lock()
	_, err = appInstance.db.Exec("UPDATE tasks SET task = ? WHERE id = ?", newTask, taskID)
	appInstance.mu.Unlock()

	if err != nil {
		http.Error(responseWriter, "Error updating task: "+err.Error(), http.StatusInternalServerError)
		return
	}

	appInstance.renderTasks(responseWriter, showCompleted)
}

func main() {
	app := &App{}

	tmpl, err := template.ParseFS(assets,
		"frontend/base.html",
		"frontend/index.html",
		"frontend/taskList.html")
	if err != nil {
		log.Fatal("Error parsing templates:", err)
	}
	app.templates = tmpl

	err = app.initializeDB()
	if err != nil {
		log.Println("Error initializing database:", err.Error())
		return
	}

	http.HandleFunc("/", app.handleIndex) // This must come first
	http.HandleFunc("/addTask", app.AddTask)
	http.HandleFunc("/getTasks", app.GetTasks)
	http.HandleFunc("/getCompletedTasks", app.GetCompletedTasks)
	http.HandleFunc("/completeTask", app.CompleteTask)
	http.HandleFunc("/deleteTask", app.DeleteTask)
	http.HandleFunc("/editTask", app.EditTask)

	log.Println("Starting HTTP server on http://localhost:8080")
	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Println("Error starting HTTP server:", err.Error())
	}
	log.Println("HTTP server stopped")
}
