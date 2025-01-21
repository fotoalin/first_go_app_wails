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

func (a *App) AddTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	err := r.ParseForm()
	if err != nil {
		http.Error(w, "Error parsing form: "+err.Error(), http.StatusBadRequest)
		return
	}

	task := r.FormValue("task")
	if task == "" {
		http.Error(w, "Task cannot be empty", http.StatusBadRequest)
		return
	}

	a.mu.Lock()
	_, err = a.db.Exec("INSERT INTO tasks (task) VALUES (?)", task)
	a.mu.Unlock()

	if err != nil {
		http.Error(w, "Error adding task: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Only render the task list template after successful insertion
	a.renderTasks(w, false)
}

func (a *App) GetTasks(w http.ResponseWriter, r *http.Request) {
	fmt.Println("GetTasks called")
	a.renderTasks(w, false)
}

func (a *App) GetCompletedTasks(w http.ResponseWriter, r *http.Request) {
	fmt.Println("GetCompletedTasks called")
	a.renderTasks(w, true)
}

func (a *App) CompleteTask(w http.ResponseWriter, r *http.Request) {
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
	isCompleted := r.FormValue("completed")
	showCompleted := r.FormValue("showCompleted")

	fmt.Printf("TaskID: %s, Completing: %s, ShowCompleted: %s\n", taskID, isCompleted, showCompleted)

	completed := isCompleted == "true"

	a.mu.Lock()
	_, err = a.db.Exec("UPDATE tasks SET completed = ? WHERE id = ?", completed, taskID)
	a.mu.Unlock()

	if err != nil {
		http.Error(w, "Error updating task: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Show the same list we were viewing (completed or uncompleted)
	a.renderTasks(w, showCompleted == "true")
}

// Add Mutex for Safety
func (a *App) renderTasks(w http.ResponseWriter, completed bool) {
	a.mu.Lock()
	defer a.mu.Unlock()

	var rows *sql.Rows
	var err error
	if completed {
		rows, err = a.db.Query("SELECT id, task, completed FROM tasks WHERE completed = 1 ORDER BY id DESC")
	} else {
		rows, err = a.db.Query("SELECT id, task, completed FROM tasks WHERE completed = 0 ORDER BY id DESC")
	}
	if err != nil {
		http.Error(w, "Error fetching tasks: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var tasks []Task
	for rows.Next() {
		var task Task
		if err := rows.Scan(&task.ID, &task.Task, &task.Completed); err != nil {
			http.Error(w, "Error scanning task: "+err.Error(), http.StatusInternalServerError)
			return
		}
		tasks = append(tasks, task)
	}

	err = a.templates.ExecuteTemplate(w, "taskList", tasks)
	if err != nil {
		http.Error(w, "Error rendering template: "+err.Error(), http.StatusInternalServerError)
	}
}

func (a *App) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	err := a.templates.ExecuteTemplate(w, "index", nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (a *App) DeleteTask(w http.ResponseWriter, r *http.Request) {
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

	a.mu.Lock()
	_, err = a.db.Exec("DELETE FROM tasks WHERE id = ?", taskID)
	a.mu.Unlock()

	if err != nil {
		http.Error(w, "Error deleting task: "+err.Error(), http.StatusInternalServerError)
		return
	}

	a.renderTasks(w, showCompleted)
}

func (a *App) EditTask(w http.ResponseWriter, r *http.Request) {
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
	newTask := r.FormValue("newTask")
	showCompleted := r.FormValue("showCompleted") == "true"

	if newTask == "" {
		http.Error(w, "Task cannot be empty", http.StatusBadRequest)
		return
	}

	a.mu.Lock()
	_, err = a.db.Exec("UPDATE tasks SET task = ? WHERE id = ?", newTask, taskID)
	a.mu.Unlock()

	if err != nil {
		http.Error(w, "Error updating task: "+err.Error(), http.StatusInternalServerError)
		return
	}

	a.renderTasks(w, showCompleted)
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
