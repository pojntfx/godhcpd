package main

import (
	constants "github.com/pojntfx/godhcpd/cmd"
	godhcpd "github.com/pojntfx/godhcpd/pkg/proto/generated"
	"github.com/pojntfx/godhcpd/pkg/svc"
	"github.com/pojntfx/godhcpd/pkg/workers"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"gitlab.com/bloom42/libs/rz-go"
	"gitlab.com/bloom42/libs/rz-go/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
)

const (
	keyPrefix         = "dhcpdd."
	configFileDefault = ""
	configFileKey     = keyPrefix + "configFile"
	listenHostPortKey = keyPrefix + "listenHostPort"
)

var rootCmd = &cobra.Command{
	Use:   "dhcpdd",
	Short: "dhcpdd is the ISC DHCP server management daemon",
	Long: `dhcpdd is the ISC DHCP server management daemon.

Find more information at:
https://pojntfx.github.io/godhcpd/`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		viper.SetEnvPrefix("dhcpdd")
		viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if !(viper.GetString(configFileKey) == configFileDefault) {
			viper.SetConfigFile(viper.GetString(configFileKey))

			if err := viper.ReadInConfig(); err != nil {
				return err
			}
		}
		binaryDir := filepath.Join(os.TempDir(), "dhcpd")

		listener, err := net.Listen("tcp", viper.GetString(listenHostPortKey))
		if err != nil {
			return err
		}

		server := grpc.NewServer()
		reflection.Register(server)

		DHCPDService := svc.DHCPDManager{
			BinaryDir:     binaryDir,
			StateDir:      filepath.Join(os.TempDir(), "godhcpd", "dhcpd"),
			DHCPDsManaged: make(map[string]*workers.DHCPD),
		}

		godhcpd.RegisterDHCPDManagerServer(server, &DHCPDService)

		interrupt := make(chan os.Signal, 2)
		signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-interrupt

			// Allow manually killing the process
			go func() {
				<-interrupt

				os.Exit(1)
			}()

			log.Info("Gracefully stopping server (this might take a few seconds)")

			msg := "Could not stop dhcp server"

			for _, DHCPD := range DHCPDService.DHCPDsManaged {
				if err := DHCPD.Stop(); err != nil {
					log.Fatal(msg, rz.Err(err))
				}
			}

			for _, DHCPD := range DHCPDService.DHCPDsManaged {
				if err := DHCPD.Wait(); err != nil {
					log.Fatal(msg, rz.Err(err))
				}
			}

			server.GracefulStop()
		}()

		if err := DHCPDService.Extract(); err != nil {
			return err
		}

		log.Info("Starting server")

		if err := server.Serve(listener); err != nil {
			return err
		}

		return nil
	},
}

func init() {
	var (
		configFileFlag string
		hostPortFlag   string
	)

	rootCmd.PersistentFlags().StringVarP(&configFileFlag, configFileKey, "f", configFileDefault, "Configuration file to use.")
	rootCmd.PersistentFlags().StringVarP(&hostPortFlag, listenHostPortKey, "l", constants.DHCPDDHostPortDefault, "TCP listen host:port.")

	if err := viper.BindPFlags(rootCmd.PersistentFlags()); err != nil {
		log.Fatal(constants.CouldNotBindFlagsErrorMessage, rz.Err(err))
	}

	viper.AutomaticEnv()
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		log.Fatal(constants.CouldNotStartRootCommandErrorMessage, rz.Err(err))
	}
}
