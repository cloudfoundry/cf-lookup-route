package main

import (
	"code.cloudfoundry.org/cli/plugin"
	"context"
	"flag"
	"fmt"
	"github.com/cloudfoundry/go-cfclient/v3/client"
	"github.com/cloudfoundry/go-cfclient/v3/config"
	"github.com/cloudfoundry/go-cfclient/v3/resource"
	"math"
	"net/url"
	"os"
	"strconv"
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

	if args[0] == "CLI-MESSAGE-UNINSTALL" {
		return
	}
	flags := flag.NewFlagSet("lookup-route", flag.ContinueOnError)
	target := flags.Bool("t", false, "Target the org/space containing this route")
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
		err = fmt.Errorf("please log in to search for apps")
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
	if route.Destinations == nil || len(route.Destinations) == 0 {
		err = fmt.Errorf("route not bound to any applications")
		return
	}

	err = lookup(cfc, route, *target, cliConnection)
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
				HelpText: "Cloud Foundry CLI plugin to identify applications, a given route is pointing to.",
				UsageDetails: plugin.Usage{
					Usage: "cf lookup-route [-t] ROUTE_URL",
					Options: map[string]string{
						"t": "Target the org/space containing the route",
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

	hostName, domainName, found := strings.Cut(routeUrl.Hostname(), ".")
	if !found {
		return &resource.Domain{}, "", routeUrl, fmt.Errorf("'%s' is not a domain", routeUrl.Hostname())
	}

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

func getBatchEndIdx(numOfRouteDest int, packLength int, currentIdx int) int {
	packEndIdx := currentIdx*packLength + packLength
	if packEndIdx > numOfRouteDest {
		packEndIdx = numOfRouteDest
	}
	return packEndIdx
}

func resolveApps(cfc *client.Client, route *resource.Route) ([]*resource.App, error) {
	var appGuids []string
	var apps []*resource.App

	for _, destination := range route.Destinations {
		appGuids = append(appGuids, *destination.App.GUID)
	}

	// Batching of app queries (to reduce cf api calls)
	routeDestCount := len(appGuids)
	batchSize := 100
	batchCount := int(math.Ceil(float64(routeDestCount) / float64(batchSize)))

	for i := 0; i < routeDestCount; i++ {
		appGuids = append(appGuids, strconv.Itoa(i))
	}
	opts := client.NewAppListOptions()

	opts.PerPage = batchSize

	for i := 0; i < batchCount; i++ {
		opts.GUIDs.Values = appGuids[i*batchSize : getBatchEndIdx(routeDestCount, batchSize, i)]
		batchApps, err := cfc.Applications.ListAll(context.Background(), opts)
		if err != nil {
			return []*resource.App{}, err
		}
		apps = append(apps, batchApps...)
	}
	return apps, nil
}

func lookup(cfc *client.Client, route *resource.Route, target bool, cliConnection plugin.CliConnection) error {
	apps, err := resolveApps(cfc, route)
	if err != nil {
		return err
	}
	if len(apps) == 0 {
		return fmt.Errorf("route not bound to any applications")
	}

	// All the apps sharing a route must be in the same org and space
	space, org, err := cfc.Spaces.GetIncludeOrganization(context.Background(), apps[0].Relationships.Space.Data.GUID)
	if err != nil {
		return err
	}

	fmt.Printf("Bound to:\nOrganization: %s (%s)\n", org.Name, org.GUID)
	fmt.Printf("Space       : %s (%s)\n", space.Name, space.GUID)
	for _, app := range apps {
		fmt.Printf("App         : %s (%s)\n", app.Name, app.GUID)
	}

	if target {
		err = targetAppSpace(org.Name, space.Name, cliConnection)
		if err != nil {
			return err
		}
	}
	return nil
}

func targetAppSpace(org string, space string, cliConnection plugin.CliConnection) error {
	fmt.Printf("Targeting an app's organization and space...\n")
	_, err := cliConnection.CliCommand("target", "-o", org, "-s", space)
	if err != nil {
		fmt.Printf("targeting an app's organization and space failed\n")
		return err
	}
	fmt.Printf("Targeting an app's organization and space successful.\n")
	return nil
}
