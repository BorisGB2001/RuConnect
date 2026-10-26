// Package pb holds the generated RustDesk rendezvous protocol messages.
package pb

//go:generate protoc --go_out=. --go_opt=paths=source_relative --go_opt=Mrendezvous.proto=github.com/BorisGB2001/RuConnect/internal/rendezvous/pb rendezvous.proto
