-- +goose Up
CREATE TABLE students (name TEXT PRIMARY KEY, metadata TEXT NOT NULL);
CREATE TABLE exercises (student_name TEXT NOT NULL REFERENCES students(name) ON DELETE CASCADE, exercise INTEGER NOT NULL, position INTEGER NOT NULL, metadata TEXT NOT NULL, PRIMARY KEY(student_name, exercise));
CREATE TABLE cases (student_name TEXT NOT NULL, exercise INTEGER NOT NULL, input_file TEXT NOT NULL, position INTEGER NOT NULL, input_size INTEGER NOT NULL, duration_ns INTEGER NOT NULL, passed BOOLEAN NOT NULL, failure_type TEXT NOT NULL, message TEXT NOT NULL, expected TEXT NOT NULL, actual TEXT NOT NULL, metadata TEXT NOT NULL, PRIMARY KEY(student_name, exercise, input_file), FOREIGN KEY(student_name, exercise) REFERENCES exercises(student_name, exercise) ON DELETE CASCADE);
-- +goose Down
DROP TABLE cases;
DROP TABLE exercises;
DROP TABLE students;
