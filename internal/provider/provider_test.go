// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/hashicorp/boundary/testing/controller"
	wrapping "github.com/hashicorp/go-kms-wrapping/v2"
	"github.com/hashicorp/go-kms-wrapping/v2/aead"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/resource"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
	"github.com/hashicorp/terraform-plugin-sdk/v2/terraform"
)

var (
	tcLoginName = "testuser"
	tcPassword  = "passpass"
	tcPAUM      = "ampw_0000000000"
	tcConfig    = []controller.Option{
		controller.WithDefaultPasswordAuthMethodId(tcPAUM),
		controller.WithDefaultLoginName(tcLoginName),
		controller.WithDefaultPassword(tcPassword),
	}
	tcRecoveryKey = "7xtkEoS5EXPbgynwd+dDLHopaCqK8cq0Rpep4eooaTs="
)

func providerFactories(p **schema.Provider) map[string]func() (*schema.Provider, error) {
	// TODO: eventually rework this to real factories...
	*p = New()
	return map[string]func() (*schema.Provider, error){
		"boundary": func() (*schema.Provider, error) {
			return *p, nil
		},
	}
}

func testWrapper(ctx context.Context, t *testing.T, key string) wrapping.Wrapper {
	var keyBytes []byte
	switch key {
	case "":
		keyBytes = make([]byte, 32)
		n, err := rand.Read(keyBytes)
		if err != nil {
			t.Fatal(err)
		}
		if n != 32 {
			t.Fatal(n)
		}
		key = base64.StdEncoding.EncodeToString(keyBytes)
	default:
		var err error
		keyBytes, err = base64.StdEncoding.DecodeString(key)
		if err != nil {
			t.Fatal(err)
		}
	}
	wrapper := aead.NewWrapper()

	_, err := wrapper.SetConfig(ctx, wrapping.WithKeyId(key))
	if err != nil {
		t.Fatal(err)
	}
	if err := wrapper.SetAesGcmKeyBytes(keyBytes); err != nil {
		t.Fatal(err)
	}
	return wrapper
}

func testConfig(url string, res ...string) string {
	provider := fmt.Sprintf(`
provider "boundary" {
	addr             = "%s"
	auth_method_id       = "%s"
	password_auth_method_login_name = "%s"
	password_auth_method_password = "%s"
}`, url, tcPAUM, tcLoginName, tcPassword)

	c := []string{provider}
	c = append(c, res...)
	return strings.Join(c, "\n")
}

func testConfigWithToken(url, token string, res ...string) string {
	provider := fmt.Sprintf(`
provider "boundary" {
	addr  = "%s"
	token = "%s"
}`, url, token)

	c := []string{provider}
	c = append(c, res...)
	return strings.Join(c, "\n")
}

func testConfigWithDefaultAuthMethod(url string, res ...string) string {
	provider := fmt.Sprintf(`
provider "boundary" {
	addr  = "%s"
	password_auth_method_login_name = "%s"
	password_auth_method_password = "%s"
}`, url, tcLoginName, tcPassword)

	c := []string{provider}
	c = append(c, res...)
	return strings.Join(c, "\n")
}

func testConfigWithOIDCAuthMethod(url string, res ...string) string {
	provider := fmt.Sprintf(`
provider "boundary" {
	addr  = "%s"
	auth_method_id = "amoidc_0000000000"
}`, url)

	c := []string{provider}
	c = append(c, res...)
	return strings.Join(c, "\n")
}

func testConfigWithoutAMPWCredentials(url string, res ...string) string {
	provider := fmt.Sprintf(`
provider "boundary" {
	addr  = "%s"
}`, url)

	c := []string{provider}
	c = append(c, res...)
	return strings.Join(c, "\n")
}

func testConfigWithRecovery(url string, res ...string) string {
	provider := fmt.Sprintf(`
provider "boundary" {
	addr             = "%s"
	auth_method_id       = "%s"
	password_auth_method_login_name = "%s"
	password_auth_method_password = "%s"
	recovery_kms_hcl = <<DOC
	kms "aead" {
		purpose = ["recovery", "config"]
		aead_type = "aes-gcm"
		key = "7xtkEoS5EXPbgynwd+dDLHopaCqK8cq0Rpep4eooaTs="
		key_id = "global_recovery"
	}
	DOC
}`, url, tcPAUM, tcLoginName, tcPassword)

	c := []string{provider}
	c = append(c, res...)
	return strings.Join(c, "\n")
}

func importStep(name string, ignore ...string) resource.TestStep {
	step := resource.TestStep{
		ResourceName:      name,
		ImportState:       true,
		ImportStateVerify: true,
	}

	if len(ignore) > 0 {
		step.ImportStateVerifyIgnore = ignore
	}

	return step
}

func TestProvider(t *testing.T) {
	if err := New().InternalValidate(); err != nil {
		t.Fatalf("err: %s", err)
	}
}

func TestConfigWithDefaultAuthMethod(t *testing.T) {
	tc := controller.NewTestController(t, tcConfig...)
	defer tc.Shutdown()
	url := tc.ApiAddrs()[0]

	var provider *schema.Provider
	resource.Test(t, resource.TestCase{
		IsUnitTest:        true,
		ProviderFactories: providerFactories(&provider),
		CheckDestroy:      testAccCheckScopeResourceDestroy(t, provider),
		Steps: []resource.TestStep{
			{
				Config: testConfigWithDefaultAuthMethod(url, fooOrg, firstProjectFoo, secondProject),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckScopeResourceExists(provider, "boundary_scope.org1"),
					testProviderTokenExists(provider),
				),
			},
		},
	})
}

func TestConfigWithoutAMPWCredentials(t *testing.T) {
	tc := controller.NewTestController(t, tcConfig...)
	defer tc.Shutdown()
	url := tc.ApiAddrs()[0]

	var provider *schema.Provider
	resource.Test(t, resource.TestCase{
		IsUnitTest:        true,
		ProviderFactories: providerFactories(&provider),
		Steps: []resource.TestStep{
			{
				Config:      testConfigWithoutAMPWCredentials(url, fooOrg, firstProjectFoo, secondProject),
				ExpectError: regexp.MustCompile("password-style auth method login name not set, please set password_auth_method_login_name on the provider"),
			},
		},
	})
}

func TestConfigWithOIDCAuthMethod(t *testing.T) {
	tc := controller.NewTestController(t, tcConfig...)
	defer tc.Shutdown()
	url := tc.ApiAddrs()[0]

	var provider *schema.Provider
	resource.Test(t, resource.TestCase{
		IsUnitTest:        true,
		ProviderFactories: providerFactories(&provider),
		Steps: []resource.TestStep{
			{
				Config:      testConfigWithOIDCAuthMethod(url, fooOrg, firstProjectFoo, secondProject),
				ExpectError: regexp.MustCompile("OIDC auth method is currently not supported by Boundary Terraform Provider. only password auth method is supported at this time"),
			},
		},
	})
}

func testProviderTokenExists(testProvider *schema.Provider) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		md := testProvider.Meta().(*metaData)
		if md.client.Token() == "" {
			return fmt.Errorf("token not set")
		}
		return nil
	}
}
