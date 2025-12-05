// Package quickstart provides simple example code for documentation.
package quickstart

import (
	"context"
	"log"
	"net/http"

	"tygor.dev/tygor"
	"tygor.dev/tygorgen"
)

// [snippet:handlers collapse]
func GetUser(ctx context.Context, req *GetUserRequest) (*User, error) {
	// Your implementation
	return nil, nil
}

func CreateUser(ctx context.Context, req *CreateUserRequest) (*User, error) {
	// Your implementation
	return nil, nil
}

// [/snippet:handlers]

// [snippet:setup-app]
func SetupApp() *tygor.App {
	app := tygor.NewApp()

	users := app.Service("Users")
	users.Register("Get", tygor.Query(GetUser))      // GET request
	users.Register("Create", tygor.Exec(CreateUser)) // POST request

	return app
}

// [/snippet:setup-app]

func exampleMain() {
	// [snippet:main]
	app := SetupApp()
	http.ListenAndServe(":8080", app.Handler())
	// [/snippet:main]
}

func exampleGeneration() {
	app := tygor.NewApp()
	// [snippet:generation]
	tygorgen.FromApp(app).ToDir("./client/src/rpc")
	// [/snippet:generation]
}

func exampleClient() {
	// [snippet:client]
	// import { createClient } from "@tygor/client";
	// import type { Manifest } from "./rpc/manifest";
	//
	// const client = createClient<Manifest>("http://localhost:8080");
	//
	// const user = await client.Users.Get({ id: "123" });
	// [/snippet:client]
}

// Keep imports used.
var (
	_ = context.Background
	_ = log.Fatal
	_ = SetupApp
	_ = exampleMain
	_ = exampleGeneration
	_ = exampleClient
)
