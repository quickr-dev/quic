package agent

type CheckoutService struct {
	config *CheckoutConfig
}

type CheckoutConfig struct {
	ZFSParentDataset string
	PostgresBinPath  string // /usr/lib/postgresql/16/bin
	StartPort        int
	EndPort          int
}

func NewCheckoutService(config *CheckoutConfig) *CheckoutService {
	return &CheckoutService{
		config: config,
	}
}