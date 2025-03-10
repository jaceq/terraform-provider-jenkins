package jenkins

import (
	"context"
	"fmt"
	"strings"

	jenkins "github.com/bndr/gojenkins"
	"github.com/hashicorp/terraform-plugin-sdk/v2/diag"
	"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

func resourceJenkinsCredentialSSH() *schema.Resource {
	return &schema.Resource{
		CreateContext: resourceJenkinsCredentialSSHCreate,
		ReadContext:   resourceJenkinsCredentialSSHRead,
		UpdateContext: resourceJenkinsCredentialSSHUpdate,
		DeleteContext: resourceJenkinsCredentialSSHDelete,
		Importer: &schema.ResourceImporter{
			StateContext: resourceJenkinsCredentialSSHImport,
		},
		Schema: map[string]*schema.Schema{
			"name": {
				Type:        schema.TypeString,
				Description: "The identifier assigned to the credentials.",
				Required:    true,
				ForceNew:    true,
			},
			"domain": {
				Type:        schema.TypeString,
				Description: "The domain namespace that the credentials will be added to.",
				Optional:    true,
				Default:     "_",
				// In-place updates should be possible, but gojenkins does not support move operations
				ForceNew: true,
			},
			"folder": {
				Type:        schema.TypeString,
				Description: "The folder namespace that the credentials will be added to.",
				Optional:    true,
				ForceNew:    true,
			},
			"scope": {
				Type:             schema.TypeString,
				Description:      "The Jenkins scope assigned to the credentials.",
				Optional:         true,
				Default:          "GLOBAL",
				ValidateDiagFunc: validateCredentialScope,
			},
			"description": {
				Type:        schema.TypeString,
				Description: "The credentials descriptive text.",
				Optional:    true,
				Default:     "Managed by Terraform",
			},
			"username": {
				Type:        schema.TypeString,
				Description: "Username",
				Required:    true,
			},
			"privatekey": {
				Type:        schema.TypeString,
				Description: "The credentials private SSH key. This is mandatory.",
				Required:    true,
				Sensitive:   true,
			},
			"passphrase": {
				Type:        schema.TypeString,
				Description: "Passphrase for SSH key.",
				Optional:    true,
				Sensitive:   true,
			},
		},
	}
}

func resourceJenkinsCredentialSSHCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	client := meta.(jenkinsClient)
	cm := client.Credentials()
	cm.Folder = formatFolderName(d.Get("folder").(string))

	// Validate that the folder exists
	if err := folderExists(ctx, client, cm.Folder); err != nil {
		return diag.FromErr(fmt.Errorf("invalid folder name '%s' specified: %w", cm.Folder, err))
	}

	cred := jenkins.SSHCredentials{
		ID:          d.Get("name").(string),
		Scope:       d.Get("scope").(string),
		Description: d.Get("description").(string),
		Username:    d.Get("username").(string),
		PrivateKeySource: &jenkins.PrivateKey{
			Class: jenkins.KeySourceDirectEntryType,
			Value: d.Get("privatekey").(string),
		},
	}

	passphrase := d.Get("passphrase").(string)
	if len(passphrase) > 0 {
		cred.Passphrase = passphrase
	}

	domain := d.Get("domain").(string)
	err := cm.Add(ctx, domain, cred)
	if err != nil {
		return diag.Errorf("Could not create ssh credentials: %s", err)
	}

	d.SetId(generateCredentialID(d.Get("folder").(string), cred.ID))
	return resourceJenkinsCredentialSSHRead(ctx, d, meta)
}

func resourceJenkinsCredentialSSHRead(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	cm := meta.(jenkinsClient).Credentials()
	cm.Folder = formatFolderName(d.Get("folder").(string))

	cred := jenkins.SSHCredentials{}
	err := cm.GetSingle(
		ctx,
		d.Get("domain").(string),
		d.Get("name").(string),
		&cred,
	)

	if err != nil {
		if strings.HasSuffix(err.Error(), "404") {
			// Job does not exist
			d.SetId("")
			return nil
		}

		return diag.Errorf("Could not read ssh credentials: %s", err)
	}

	d.SetId(generateCredentialID(d.Get("folder").(string), cred.ID))
	d.Set("scope", cred.Scope)
	d.Set("description", cred.Description)
	// NOTE: We are NOT setting the secret here, as the secret returned by GetSingle is garbage
	// Secret only applies to Create/Update operations if the "password" property is non-empty

	return nil
}

func resourceJenkinsCredentialSSHUpdate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	cm := meta.(jenkinsClient).Credentials()
	cm.Folder = formatFolderName(d.Get("folder").(string))

	domain := d.Get("domain").(string)

	cred := jenkins.SSHCredentials{
		ID:          d.Get("name").(string),
		Scope:       d.Get("scope").(string),
		Description: d.Get("description").(string),
		Username:    d.Get("username").(string),
		PrivateKeySource: &jenkins.PrivateKey{
			Class: jenkins.KeySourceDirectEntryType,
			Value: d.Get("privatekey").(string),
		},
	}

	passphrase := d.Get("passphrase").(string)
	if len(passphrase) > 0 {
		cred.Passphrase = passphrase
	}

	err := cm.Update(ctx, domain, d.Get("name").(string), &cred)
	if err != nil {
		return diag.Errorf("Could not update secret text: %s", err)
	}

	d.SetId(generateCredentialID(d.Get("folder").(string), cred.ID))
	return resourceJenkinsCredentialSSHRead(ctx, d, meta)
}

func resourceJenkinsCredentialSSHDelete(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {
	cm := meta.(jenkinsClient).Credentials()
	cm.Folder = formatFolderName(d.Get("folder").(string))

	err := cm.Delete(
		ctx,
		d.Get("domain").(string),
		d.Get("name").(string),
	)
	if err != nil {
		return diag.FromErr(err)
	}

	return nil
}

func resourceJenkinsCredentialSSHImport(ctx context.Context, d *schema.ResourceData, m interface{}) ([]*schema.ResourceData, error) {
	ret := []*schema.ResourceData{d}

	splitID := strings.Split(d.Id(), "/")
	if len(splitID) < 2 {
		return ret, fmt.Errorf("import ID was improperly formatted. Imports need to be in the format \"[<folder>/]<domain>/<name>\"")
	}

	name := splitID[len(splitID)-1]
	d.Set("name", name)

	domain := splitID[len(splitID)-2]
	d.Set("domain", domain)

	folder := strings.Trim(strings.Join(splitID[0:len(splitID)-2], "/"), "/")
	d.Set("folder", folder)

	d.SetId(generateCredentialID(folder, name))
	return ret, nil
}
