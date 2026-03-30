``` lua
local run_opts = {
    cwd = function() return vim.fn.expand("%:p:h") end,
    win = {
      border = "rounded",
      wo = {
        winhighlight = "FloatBorder:DiagnosticInfo",
      },
    },
  }

  local function run_cmd(cmd)
    local opts = vim.deepcopy(run_opts)
    opts.cwd = run_opts.cwd()
    Snacks.terminal(cmd .. "; echo ''; echo 'Press Enter to close...'; read", opts)
  end

  return {
    {
      "folke/snacks.nvim",
      keys = {
        {
          "<leader>rr",
          function() run_cmd("go run " .. vim.fn.expand("%:t")) end,
          desc = "Run current Go file",
          ft = "go",
        },
        {
          "<leader>ra",
          function() run_cmd("go run .") end,
          desc = "Run Go package",
          ft = "go",
        },
        {
          "<leader>ri",
          function()
            vim.ui.input({ prompt = "Entrypoint: ", default = vim.fn.expand("%:t") },
  function(input)
              if input then run_cmd("go run " .. input) end
            end)
          end,
          desc = "Run Go file (input)",
          ft = "go",
        },
      },
    },
  }
```

  Keymaps:

  
  │ Space r r │ Ejecuta el archivo actual        │
  │ Space r a │ Ejecuta el paquete (go run .)    │
  │ Space r i │ Te pregunta qué archivo ejecutar │
