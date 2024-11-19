package main

import (
	"code.cloudfoundry.org/cli/plugin"
	"context"
	"flag"
	"fmt"
	"github.com/cloudfoundry/go-cfclient/v3/client"
	"github.com/cloudfoundry/go-cfclient/v3/config"
	"github.com/cloudfoundry/go-cfclient/v3/resource"
	"net/url"
	"os"
	"strings"
)

type lookupRoute struct{}

func main() {
	plugin.Start(new(lookupRoute))
}

func (l lookupRoute) Run(cliConnection plugin.CliConnection, args []string) {
	var err error
	defer func() {
		if err != nil {
			fmt.Printf("error: %s\n", err.Error())
			os.Exit(1)
		}
	}()

	flags := flag.NewFlagSet("lookup-route", flag.ContinueOnError)
	target := flags.Bool("t", false, "Target the org/space containing this route")
	onlyFirstResult := flags.Bool("o", false, "Retrieve only one application for this route")
	err = flags.Parse(args[1:])
	if err != nil {
		return
	}

	if len(flags.Args()) == 0 || len(args) == 0 {
		err = fmt.Errorf("please specify the required parameters")
		return
	}
	hostName := flags.Args()[0]

	hasApiEndpoint, err := cliConnection.HasAPIEndpoint()
	if err != nil || !hasApiEndpoint {
		err = fmt.Errorf("no API endpoint set")
		return
	}

	loggedIn, err := cliConnection.IsLoggedIn()
	if err != nil {
		return
	}
	if !loggedIn {
		err = fmt.Errorf("error: please log in to search for apps")
		return
	}

	cfc, err := createCfClient()
	if err != nil {
		return
	}

	route, err := findRoute(cfc, hostName)
	if err != nil {
		return
	}

	err = lookup(cfc, route, *target, *onlyFirstResult, cliConnection)
	if err != nil {
		return
	}
}

func (l lookupRoute) GetMetadata() plugin.PluginMetadata {
	return plugin.PluginMetadata{
		Name: "lookup-route",
		Version: plugin.VersionType{
			Major: 0,
			Minor: 1,
			Build: 0,
		},
		Commands: []plugin.Command{
			{
				Name:     "lookup-route",
				HelpText: "Cloud Foundry CLI plugin to identify the application, a given route is pointing to.",
				UsageDetails: plugin.Usage{
					Usage: "cf lookup-route [-t] ROUTE_URL",
					Options: map[string]string{
						"t": "Target the org/space containing the route",
						"o": "Query only the first result",
					},
				},
			},
		},
	}
}

func createCfClient() (*client.Client, error) {
	cfg, err := config.NewFromCFHome()
	if err != nil {
		return &client.Client{}, err
	}

	cfc, err := client.New(cfg)
	if err != nil {
		return &client.Client{}, err
	}

	return cfc, nil
}

func retrieveDomains(cfc *client.Client, domainName string) ([]*resource.Domain, error) {
	domainOpts := client.NewDomainListOptions()
	domainOpts.Names.Values = append(domainOpts.Names.Values, domainName)
	domains, err := cfc.Domains.ListAll(context.Background(), domainOpts)
	if err != nil {
		return nil, err
	}
	return domains, nil
}

func parseDomain(cfc *client.Client, query string) (*resource.Domain, string, *url.URL, error) {
	routeUrl, err := url.Parse(query)
	if err != nil {
		return &resource.Domain{}, "", &url.URL{}, err
	}
	if routeUrl.Scheme == "" {
		return &resource.Domain{}, "", routeUrl, fmt.Errorf("please provide the url including the scheme")
	}

	domains, err := retrieveDomains(cfc, routeUrl.Hostname())
	if err != nil {
		return &resource.Domain{}, routeUrl.Hostname(), routeUrl, err
	}

	if len(domains) > 0 {
		return domains[0], routeUrl.Hostname(), routeUrl, nil
	}

	urlParts := strings.SplitN(routeUrl.Hostname(), ".", 2)
	if len(urlParts) < 2 {
		return &resource.Domain{}, "", routeUrl, fmt.Errorf("'%s' is not a domain", routeUrl.Hostname())
	}
	hostName := urlParts[0] //Subdomain is not empty
	domainName := urlParts[1]
	domains, err = retrieveDomains(cfc, domainName)
	if len(domains) == 0 {
		return &resource.Domain{}, hostName, routeUrl, fmt.Errorf("error retrieving apps: route not found, domain '%s' is unknown", domainName)
	}

	return domains[0], hostName, routeUrl, nil
}

func findRoute(cfc *client.Client, query string) (*resource.Route, error) {
	domain, hostName, routeUrl, err := parseDomain(cfc, query)

	opts := client.NewRouteListOptions()
	opts.Hosts.Values = append(opts.Hosts.Values, hostName)
	opts.DomainGUIDs.Values = append(opts.DomainGUIDs.Values, domain.GUID)
	opts.Paths.Values = append(opts.Paths.Values, routeUrl.Path)

	routes, err := cfc.Routes.ListAll(context.Background(), opts)
	if err != nil {
		return &resource.Route{}, err
	}

	if len(routes) > 0 {
		return routes[0], nil
	}
	// Wildcard search
	opts.Hosts.Values = append(opts.Hosts.Values, "*")
	routes, err = cfc.Routes.ListAll(context.Background(), opts)
	if err != nil {
		return &resource.Route{}, err
	}
	if len(routes) == 0 {
		return &resource.Route{}, fmt.Errorf("error retrieving apps: route '%s' not found", routeUrl.Hostname())
	}

	return routes[0], nil
}

func lookup(cfc *client.Client, route *resource.Route, target bool, onlyFirstResult bool, cliConnection plugin.CliConnection) error {
	if route.Destinations == nil || len(route.Destinations) == 0 {
		return fmt.Errorf("route not bound to any applications")
	}

	for destinationCount, destination := range route.Destinations {
		app, err := cfc.Applications.Get(context.Background(), *destination.App.GUID)
		if err != nil {
			return fmt.Errorf("route not bound to any applications")
		}

		space, org, err := cfc.Spaces.GetIncludeOrganization(context.Background(), app.Relationships.Space.Data.GUID)
		if err != nil {
			return err
		}
		if destinationCount == 0 {
			fmt.Printf("Bound to:\nOrganization: %s (%s)\n", org.Name, org.GUID)
			fmt.Printf("Space       : %s (%s)\n", space.Name, space.GUID)
		}
		fmt.Printf("App         : %s (%s)\n", app.Name, app.GUID)
		if target && (destinationCount == len(route.Destinations)-1 || onlyFirstResult) {
			fmt.Printf("Targeting an app's organization and space...\n")
			_, err := cliConnection.CliCommand("target", "-o", org.Name, "-s", space.Name)
			if err != nil {
				fmt.Printf("targeting an app's organization and space failed\n")
			}
			fmt.Printf("Targeting an app's organization and space successful.\n")
		}
		if destinationCount == 0 && onlyFirstResult {
			break
		}
	}
	return nil
}
