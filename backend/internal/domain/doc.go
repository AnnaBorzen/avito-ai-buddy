// Package domain contains the business rules of the access-request scenario.
//
// The package is deliberately infrastructure-free: it does not import database
// drivers, HTTP or JSON packages. Services and repositories translate domain
// errors and values into transport/database representations.
package domain
